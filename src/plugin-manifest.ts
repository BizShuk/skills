import { readFile, readdir, stat } from 'fs/promises';
import { join, dirname, resolve, normalize, sep } from 'path';
import { parseSource, getOwnerRepo, parseOwnerRepo } from './source-parser.ts';

/**
 * Derive an `owner/repo` string from a raw git URL via the existing
 * source-parser utilities. Returns null if the URL isn't GitHub/GitLab/SHA-
 * style with a clear path component. Used by the marketplace walker to keep
 * `ownerRepo` populated for non-github sources so cycle detection works
 * regardless of source type.
 */
function deriveOwnerRepoFromUrl(url: string): string | null {
  try {
    const parsed = parseSource(url);
    const ownerRepo = getOwnerRepo(parsed);
    return ownerRepo && ownerRepo.includes('/') ? ownerRepo.toLowerCase() : null;
  } catch {
    return null;
  }
}

/**
 * Check if a path is contained within a base directory.
 * Prevents path traversal attacks via `..` segments or absolute paths.
 */
function isContainedIn(targetPath: string, basePath: string): boolean {
  const normalizedBase = normalize(resolve(basePath));
  const normalizedTarget = normalize(resolve(targetPath));
  return normalizedTarget.startsWith(normalizedBase + sep) || normalizedTarget === normalizedBase;
}

/**
 * Validate that a relative path follows Claude Code conventions.
 * Paths must start with './' per the plugin manifest spec.
 */
function isValidRelativePath(path: string): boolean {
  return path.startsWith('./');
}

/**
 * Plugin manifest types
 */
interface PluginManifestEntry {
  source?: string | { source?: string; repo?: string };
  skills?: string[];
  /** Optional name for grouping skills (e.g., "document-skills") */
  name?: string;
}

interface MarketplaceManifest {
  metadata?: { pluginRoot?: string };
  plugins?: PluginManifestEntry[];
}

interface PluginManifest {
  skills?: string[];
  name?: string;
}

/**
 * Strict JSON parser for marketplace.json content.
 *
 * Returns a parsed MarketplaceManifest on success, or null when the content
 * is empty, malformed, or not a JSON object. Pure function — no filesystem
 * dependency, so it's safe to use from blob-fetch paths where the manifest
 * arrives over the network.
 */
export function parseMarketplaceJson(content: string): MarketplaceManifest | null {
  if (!content || typeof content !== 'string') return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(content);
  } catch {
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return null;
  }
  return parsed as MarketplaceManifest;
}

/**
 * Result of resolving a plugin's `source` field against the parent marketplace.
 *
 * - `local`: a relative path inside the parent repo (e.g., "./plugins/foo")
 * - `remote`: another git repo; caller should recurse into it
 * - `fallback`: source was omitted — use the parent repo itself
 * - `null`: source is invalid and should be skipped
 *
 * The `remote` variant always carries `ownerRepo` so cycle detection and BFS
 * traversal in the marketplace walker keep working. `url`, `ref`, `sha`, and
 * `subdir` are populated when the source type supplies them:
 *
 *   - github source    → ownerRepo (+ optional ref/sha)
 *   - url source       → ownerRepo + url (+ optional ref/sha)
 *   - git-subdir src   → ownerRepo + url + subdir (+ optional ref/sha)
 */
export type ResolvedPluginSource =
  | { kind: 'local'; path: string }
  | {
      kind: 'remote';
      ownerRepo: string;
      /** Full git URL, present for url / git-subdir sources */
      url?: string;
      /** Pinned ref (branch or tag) */
      ref?: string;
      /** Pinned commit SHA — stronger than ref */
      sha?: string;
      /** Subdir within the repo (git-subdir source) */
      subdir?: string;
    }
  | { kind: 'fallback'; ownerRepo: string }
  | null;

/**
 * Map a marketplace plugin's `source` field to a normalized, actionable form.
 *
 * Supports:
 *   - undefined                       → fallback to the parent marketplace repo
 *   - "./relative/path"               → local, use as repo-relative path
 *   - "owner/repo" shorthand          → remote github
 *   - "github:owner/repo"             → remote github
 *   - "https://host/owner/repo"       → remote (github / gitlab / git)
 *   - "git@host:owner/repo.git"       → remote git
 *   - { source:'github', repo,        → remote github (ref/sha preserved)
 *       ref?, sha? }
 *   - { source:'url', url,            → remote (ownerRepo parsed from url,
 *       ref?, sha? }                    ref/sha preserved)
 *   - { source:'git-subdir',          → remote + subdir (ownerRepo from url,
 *       url, path, ref?, sha? }         ref/sha preserved)
 *
 * Invalid inputs return null so the caller can skip the plugin cleanly.
 */
export function resolvePluginSource(
  sourceValue:
    | string
    | {
        source?: string;
        repo?: string;
        url?: string;
        path?: string;
        ref?: string;
        sha?: string;
      }
    | undefined,
  fallbackOwnerRepo: string
): ResolvedPluginSource {
  if (sourceValue === undefined) {
    return { kind: 'fallback', ownerRepo: fallbackOwnerRepo.toLowerCase() };
  }

  // Object form: dispatch on the explicit `source` discriminator, then fall
  // back to a `{ repo }`-only legacy form.
  if (typeof sourceValue === 'object' && sourceValue !== null) {
    const ref = typeof sourceValue.ref === 'string' ? sourceValue.ref : undefined;
    const sha = typeof sourceValue.sha === 'string' ? sourceValue.sha : undefined;
    const sourceType = sourceValue.source;

    // git-subdir: { source:'git-subdir', url, path, ref?, sha? }
    if (sourceType === 'git-subdir') {
      const url = sourceValue.url;
      const subpath = sourceValue.path;
      if (typeof url !== 'string' || typeof subpath !== 'string') return null;
      const ownerRepo = deriveOwnerRepoFromUrl(url);
      if (!ownerRepo) return null;
      return {
        kind: 'remote',
        ownerRepo,
        url,
        subdir: subpath,
        ...(ref ? { ref } : {}),
        ...(sha ? { sha } : {}),
      };
    }

    // url: { source:'url', url, ref?, sha? }
    if (sourceType === 'url') {
      const url = sourceValue.url;
      if (typeof url !== 'string') return null;
      const ownerRepo = deriveOwnerRepoFromUrl(url);
      if (!ownerRepo) return null;
      return {
        kind: 'remote',
        ownerRepo,
        url,
        ...(ref ? { ref } : {}),
        ...(sha ? { sha } : {}),
      };
    }

    // github (or legacy { repo }): { source:'github', repo, ref?, sha? }
    const repoStr = sourceValue.repo;
    const parsed = typeof repoStr === 'string' ? parseOwnerRepo(repoStr) : null;
    if (parsed) {
      return {
        kind: 'remote',
        ownerRepo: `${parsed.owner}/${parsed.repo}`.toLowerCase(),
        ...(ref ? { ref } : {}),
        ...(sha ? { sha } : {}),
      };
    }
    return null;
  }

  if (typeof sourceValue !== 'string') return null;

  // Local relative path (per Claude Code convention)
  if (sourceValue.startsWith('./') || sourceValue.startsWith('../')) {
    return { kind: 'local', path: sourceValue };
  }

  // Anything else — try to parse as a git source (github / gitlab / git)
  try {
    const parsed = parseSource(sourceValue);
    if (parsed.type === 'well-known' || parsed.type === 'local') return null;
    const ownerRepo = getOwnerRepo(parsed);
    if (ownerRepo && ownerRepo.includes('/')) {
      return { kind: 'remote', ownerRepo: ownerRepo.toLowerCase() };
    }
  } catch {
    // parseSource swallows internal errors, but be defensive
  }

  return null;
}

/**
 * Extract skill search directories from plugin manifests.
 * Handles both marketplace.json (multi-plugin) and plugin.json (single plugin).
 * Only resolves local paths - remote sources are skipped.
 *
 * Returns directories that CONTAIN skills (to be searched for child SKILL.md files).
 * For explicit skill paths in manifests, adds the parent directory so the
 * existing discovery loop finds them.
 */
export async function getPluginSkillPaths(basePath: string): Promise<string[]> {
  const searchDirs: string[] = [];

  // Helper: add skill paths for a plugin at a given base path
  // Only adds paths that are contained within basePath (security: prevents traversal)
  const addPluginSkillPaths = (pluginBase: string, skills?: string[]) => {
    // Validate pluginBase itself is contained
    if (!isContainedIn(pluginBase, basePath)) return;

    if (skills && skills.length > 0) {
      // Plugin explicitly declares skill paths - add parent dirs so existing loop finds them
      for (const skillPath of skills) {
        // Validate skill path starts with './' (per Claude Code convention)
        if (!isValidRelativePath(skillPath)) continue;

        const skillDir = dirname(join(pluginBase, skillPath));
        if (isContainedIn(skillDir, basePath)) {
          searchDirs.push(skillDir);
        }
      }
    }
    // Always add conventional skills/ directory for discovery
    // (deduplication happens via seenNames in discoverSkills)
    searchDirs.push(join(pluginBase, 'skills'));
  };

  // Try marketplace.json (multi-plugin catalog)
  try {
    const content = await readFile(join(basePath, '.claude-plugin/marketplace.json'), 'utf-8');
    const manifest = parseMarketplaceJson(content);
    if (manifest) {
      const pluginRoot = manifest.metadata?.pluginRoot;

      // Validate pluginRoot starts with './' if provided (per Claude Code convention)
      const validPluginRoot = pluginRoot === undefined || isValidRelativePath(pluginRoot);

      if (validPluginRoot) {
        for (const plugin of manifest.plugins ?? []) {
          // Skip remote sources (object with source/repo) - only handle local string paths
          if (typeof plugin.source !== 'string' && plugin.source !== undefined) continue;

          // Validate source starts with './' if provided (per Claude Code convention)
          if (plugin.source !== undefined && !isValidRelativePath(plugin.source)) continue;

          const pluginBase = join(basePath, pluginRoot ?? '', plugin.source ?? '');
          addPluginSkillPaths(pluginBase, plugin.skills);
        }
      }
    }
  } catch {
    // File doesn't exist
  }

  // Try plugin.json (single plugin at root)
  try {
    const content = await readFile(join(basePath, '.claude-plugin/plugin.json'), 'utf-8');
    const manifest: PluginManifest = JSON.parse(content);
    addPluginSkillPaths(basePath, manifest.skills);
  } catch {
    // File doesn't exist or invalid JSON
  }

  return searchDirs;
}

async function hasSkillMd(dir: string): Promise<boolean> {
  try {
    const s = await stat(join(dir, 'SKILL.md'));
    return s.isFile();
  } catch {
    return false;
  }
}

/**
 * Get a map of skill directory paths to plugin names from plugin manifests.
 * This allows grouping skills by their parent plugin.
 *
 * Returns Map<AbsolutePath, PluginName>
 */
export async function getPluginGroupings(basePath: string): Promise<Map<string, string>> {
  const groupings = new Map<string, string>();

  // Try marketplace.json (multi-plugin catalog)
  try {
    const content = await readFile(join(basePath, '.claude-plugin/marketplace.json'), 'utf-8');
    const manifest = parseMarketplaceJson(content);
    if (manifest) {
      const pluginRoot = manifest.metadata?.pluginRoot;

      // Validate pluginRoot starts with './' if provided (per Claude Code convention)
      const validPluginRoot = pluginRoot === undefined || isValidRelativePath(pluginRoot);

      if (validPluginRoot) {
        for (const plugin of manifest.plugins ?? []) {
          if (!plugin.name) continue;

          // Skip remote sources (object with source/repo) - only handle local string paths
          if (typeof plugin.source !== 'string' && plugin.source !== undefined) continue;

          // Validate source starts with './' if provided (per Claude Code convention)
          if (plugin.source !== undefined && !isValidRelativePath(plugin.source)) continue;

          const pluginBase = join(basePath, pluginRoot ?? '', plugin.source ?? '');

          // Validate pluginBase itself is contained
          if (!isContainedIn(pluginBase, basePath)) continue;

          if (plugin.skills && plugin.skills.length > 0) {
            for (const skillPath of plugin.skills) {
              // Validate skill path starts with './' (per Claude Code convention)
              if (!isValidRelativePath(skillPath)) continue;

              const skillDir = join(pluginBase, skillPath);
              if (isContainedIn(skillDir, basePath)) {
                // Store absolute path as key for reliable matching
                groupings.set(resolve(skillDir), plugin.name);
              }
            }
          }

          // Map conventional skills under the plugin's skills/ directory
          const conventionalSkillsDir = join(pluginBase, 'skills');
          try {
            const entries = await readdir(conventionalSkillsDir, { withFileTypes: true });
            for (const entry of entries) {
              if (entry.isDirectory()) {
                const skillDir = join(conventionalSkillsDir, entry.name);
                if (await hasSkillMd(skillDir)) {
                  groupings.set(resolve(skillDir), plugin.name);
                } else {
                  try {
                    const subEntries = await readdir(skillDir, { withFileTypes: true });
                    for (const subEntry of subEntries) {
                      if (subEntry.isDirectory()) {
                        const subSkillDir = join(skillDir, subEntry.name);
                        if (await hasSkillMd(subSkillDir)) {
                          groupings.set(resolve(subSkillDir), plugin.name);
                        }
                      }
                    }
                  } catch {}
                }
              }
            }
          } catch {}
        }
      }
    }
  } catch {
    // File doesn't exist
  }

  // Try plugin.json (single plugin at root)
  try {
    const content = await readFile(join(basePath, '.claude-plugin/plugin.json'), 'utf-8');
    const manifest: PluginManifest = JSON.parse(content);
    if (manifest.name) {
      if (manifest.skills && manifest.skills.length > 0) {
        for (const skillPath of manifest.skills) {
          if (!isValidRelativePath(skillPath)) continue;
          const skillDir = join(basePath, skillPath);
          if (isContainedIn(skillDir, basePath)) {
            groupings.set(resolve(skillDir), manifest.name);
          }
        }
      }

      // Map conventional skills under the plugin's skills/ directory
      const conventionalSkillsDir = join(basePath, 'skills');
      try {
        const entries = await readdir(conventionalSkillsDir, { withFileTypes: true });
        for (const entry of entries) {
          if (entry.isDirectory()) {
            const skillDir = join(conventionalSkillsDir, entry.name);
            if (await hasSkillMd(skillDir)) {
              groupings.set(resolve(skillDir), manifest.name);
            } else {
              try {
                const subEntries = await readdir(skillDir, { withFileTypes: true });
                for (const subEntry of subEntries) {
                  if (subEntry.isDirectory()) {
                    const subSkillDir = join(skillDir, subEntry.name);
                    if (await hasSkillMd(subSkillDir)) {
                      groupings.set(resolve(subSkillDir), manifest.name);
                    }
                  }
                }
              } catch {}
            }
          }
        }
      } catch {}
    }
  } catch {
    // File doesn't exist or invalid JSON
  }

  return groupings;
}

/**
 * A remote-source plugin entry from a local marketplace.json.
 *
 * Plugins whose `source` field is an object (e.g. `{source: "github", repo: "owner/name"}`)
 * point at an external repository. The local skills CLI cannot discover their
 * skills on disk — callers must fetch them via the blob path (see
 * `tryMarketplaceBlobInstall` in blob.ts) using `ownerRepo` + optional `ref`/`sha`.
 */
export interface MarketplaceRemotePlugin {
  pluginName: string;
  ownerRepo: string;
  ref?: string;
  sha?: string;
}

/**
 * Collect every github/url/git-subdir source plugin from a local marketplace.json.
 *
 * Local-source plugins (string `source` paths) are skipped — those are handled
 * by `getPluginSkillPaths` / `getPluginGroupings`. Only object-form sources
 * pointing at remote repositories are returned, so callers can fetch their
 * skills via the blob install path.
 *
 * Returns an empty array when:
 *   - the marketplace.json file is missing or malformed
 *   - the manifest declares no plugins
 *   - every plugin uses a local string source
 */
export async function getMarketplaceRemotePlugins(
  basePath: string
): Promise<MarketplaceRemotePlugin[]> {
  const out: MarketplaceRemotePlugin[] = [];
  try {
    const content = await readFile(join(basePath, '.claude-plugin/marketplace.json'), 'utf-8');
    const manifest = parseMarketplaceJson(content);
    if (!manifest?.plugins) return out;

    for (const plugin of manifest.plugins) {
      if (!plugin.name) continue;
      // Only object-form sources (remote). String paths are local.
      if (typeof plugin.source !== 'object' || plugin.source === null) continue;

      const resolved = resolvePluginSource(plugin.source, '');
      if (!resolved || resolved.kind !== 'remote') continue;

      out.push({
        pluginName: plugin.name,
        ownerRepo: resolved.ownerRepo,
        ...(resolved.ref ? { ref: resolved.ref } : {}),
        ...(resolved.sha ? { sha: resolved.sha } : {}),
      });
    }
  } catch {
    // File doesn't exist or invalid JSON — return empty list
  }
  return out;
}
