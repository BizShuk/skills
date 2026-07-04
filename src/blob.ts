/**
 * Blob-based skill download utilities.
 *
 * Enables fast skill installation by fetching pre-built skill snapshots
 * from the skills.sh download API instead of cloning git repos.
 *
 * Flow:
 *   1. GitHub Trees API → discover SKILL.md locations
 *   2. raw.githubusercontent.com → fetch frontmatter to get skill names
 *   3. skills.sh/api/download → fetch full file contents from cached blob
 */

import { createHash } from 'node:crypto';
import { basename, dirname, join } from 'path';
import { parseFrontmatter } from './frontmatter.ts';
import { sanitizeMetadata } from './sanitize.ts';
import { parseMarketplaceJson, resolvePluginSource } from './plugin-manifest.ts';
import type { Skill } from './types.ts';

// ─── Types ───

export interface SkillSnapshotFile {
  path: string;
  contents: string;
}

export interface SkillDownloadResponse {
  files: SkillSnapshotFile[];
  hash: string; // skillsComputedHash
}

/**
 * A skill resolved from blob storage, carrying file contents in memory
 * instead of referencing a directory on disk.
 */
export interface BlobSkill extends Skill {
  /** Files from the blob snapshot */
  files: SkillSnapshotFile[];
  /** skillsComputedHash from the blob snapshot */
  snapshotHash: string;
  /** Path of the SKILL.md within the repo (e.g., "skills/react-best-practices/SKILL.md") */
  repoPath: string;
}

// ─── Constants ───

const DOWNLOAD_BASE_URL = process.env.SKILLS_DOWNLOAD_URL || 'https://skills.sh';

// Repos that self-host their downloads on the blob fast-path
export const BLOB_ALLOWED_REPOS: Record<string, { downloadUrl: (slug: string) => string }> = {
  'zapier/connectors': {
    downloadUrl: (slug) =>
      `https://connectors-skills.zapier.com/download/${encodeURIComponent(slug)}/snapshot.json`,
  },
};

/** Timeout for individual HTTP fetches (ms) */
const FETCH_TIMEOUT = 10_000;

// ─── Slug computation ───

/**
 * Convert a skill name to a URL-safe slug.
 * Must match the server-side toSkillSlug() exactly.
 */
export function toSkillSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[\s_]+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

// ─── GitHub Trees API ───

export interface TreeEntry {
  path: string;
  type: 'blob' | 'tree';
  sha: string;
  size?: number;
}

export interface RepoTree {
  sha: string;
  branch: string;
  tree: TreeEntry[];
}

/**
 * Within-process memo: once we've discovered that the GitHub API has
 * rate-limited this IP, subsequent calls skip straight to the auth fallback
 * instead of burning round trips on requests guaranteed to 403.
 */
let _rateLimitedThisSession = false;

/** For tests only. */
export function resetRepoTreeAuthState(): void {
  _rateLimitedThisSession = false;
}

interface BranchFetchResult {
  tree: RepoTree | null;
  rateLimited: boolean;
}

async function fetchTreeBranch(
  ownerRepo: string,
  branch: string,
  token: string | null
): Promise<BranchFetchResult> {
  try {
    const url = `https://api.github.com/repos/${ownerRepo}/git/trees/${encodeURIComponent(branch)}?recursive=1`;
    const headers: Record<string, string> = {
      Accept: 'application/vnd.github.v3+json',
      'User-Agent': 'skills-cli',
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    const response = await fetch(url, {
      headers,
      signal: AbortSignal.timeout(FETCH_TIMEOUT),
    });

    if (response.ok) {
      const data = (await response.json()) as {
        sha: string;
        tree: TreeEntry[];
      };
      return {
        tree: { sha: data.sha, branch, tree: data.tree },
        rateLimited: false,
      };
    }

    // GitHub signals rate-limit with 403 + X-RateLimit-Remaining: 0.
    // (A bare 403 means permission denied, which is not retryable here.)
    const rateLimited =
      response.status === 403 && response.headers.get('x-ratelimit-remaining') === '0';
    return { tree: null, rateLimited };
  } catch {
    return { tree: null, rateLimited: false };
  }
}

/**
 * Fetch the full recursive tree for a GitHub repo.
 * Returns the tree data including all entries, or null on failure.
 * Tries branches in order: ref (if specified), then main, then master.
 *
 * Authentication is lazy: by default the call goes out unauthenticated,
 * which is enough for the vast majority of users (60 req/hr per IP).
 * Only if GitHub responds with a rate-limit 403 do we ask the optional
 * `getToken` callback for a token and retry. This avoids invoking
 * `gh auth token` on every install, which corporate endpoint security
 * tools flag as suspicious credential extraction. See issue #523.
 */
export async function fetchRepoTree(
  ownerRepo: string,
  ref?: string,
  getToken?: () => string | null
): Promise<RepoTree | null> {
  const branches = ref ? [ref] : ['HEAD', 'main', 'master'];

  // Fast path: once we've seen a rate limit in this process, don't bother
  // retrying unauth on subsequent calls. Go straight to auth.
  if (_rateLimitedThisSession && getToken) {
    const token = getToken();
    if (!token) return null;
    for (const branch of branches) {
      const result = await fetchTreeBranch(ownerRepo, branch, token);
      if (result.tree) return result.tree;
    }
    return null;
  }

  // First pass: unauthenticated.
  let rateLimited = false;
  for (const branch of branches) {
    const result = await fetchTreeBranch(ownerRepo, branch, null);
    if (result.tree) return result.tree;
    if (result.rateLimited) {
      // All branches share the same rate-limit bucket on this IP, so it's
      // pointless to keep trying other branches in this pass.
      rateLimited = true;
      break;
    }
  }

  if (!rateLimited || !getToken) return null;

  // Lazy fallback: rate limit hit and a token resolver was provided.
  _rateLimitedThisSession = true;
  const token = getToken();
  if (!token) return null;

  for (const branch of branches) {
    const result = await fetchTreeBranch(ownerRepo, branch, token);
    if (result.tree) return result.tree;
  }
  return null;
}

/**
 * Extract the folder hash (tree SHA) for a specific skill path from a repo tree.
 * This replaces the per-skill GitHub API call previously done in fetchSkillFolderHash().
 */
export function getSkillFolderHashFromTree(tree: RepoTree, skillPath: string): string | null {
  let folderPath = skillPath.replace(/\\/g, '/');

  // Remove SKILL.md suffix to get folder path (case-insensitive)
  if (folderPath.toLowerCase().endsWith('/skill.md')) {
    folderPath = folderPath.slice(0, -9);
  } else if (folderPath.toLowerCase().endsWith('skill.md')) {
    folderPath = folderPath.slice(0, -8);
  }
  if (folderPath.endsWith('/')) {
    folderPath = folderPath.slice(0, -1);
  }

  // Root-level skill
  if (!folderPath) {
    return tree.sha;
  }

  const entry = tree.tree.find((e) => e.type === 'tree' && e.path === folderPath);
  return entry?.sha ?? null;
}

// ─── Skill discovery from tree ───

/** Known directories where SKILL.md files are commonly found (relative to repo root) */
const PRIORITY_PREFIXES = [
  '',
  'skills/',
  'skills/.curated/',
  'skills/.experimental/',
  'skills/.system/',
  '.agents/skills/',
  '.claude/skills/',
  '.cline/skills/',
  '.codebuddy/skills/',
  '.codex/skills/',
  '.commandcode/skills/',
  '.continue/skills/',
  '.github/skills/',
  '.goose/skills/',
  '.iflow/skills/',
  '.junie/skills/',
  '.kilocode/skills/',
  '.kiro/skills/',
  '.mux/skills/',
  '.neovate/skills/',
  '.opencode/skills/',
  '.openhands/skills/',
  '.pi/skills/',
  '.qoder/skills/',
  '.roo/skills/',
  '.trae/skills/',
  '.windsurf/skills/',
  '.zencoder/skills/',
];

/**
 * Find all SKILL.md file paths in a repo tree.
 * Applies the same priority directory logic as discoverSkills().
 * If subpath is set, only searches within that subtree.
 */
export function findSkillMdPaths(tree: RepoTree, subpath?: string): string[] {
  // Find all blob entries that are SKILL.md files (case-insensitive)
  const allSkillMds = tree.tree
    .filter((e) => e.type === 'blob' && e.path.toLowerCase().endsWith('skill.md'))
    .map((e) => e.path);

  // Apply subpath filter
  const prefix = subpath ? (subpath.endsWith('/') ? subpath : subpath + '/') : '';
  const filtered = prefix
    ? allSkillMds.filter((p) => p.startsWith(prefix) || p === prefix + 'SKILL.md')
    : allSkillMds;

  if (filtered.length === 0) return [];

  // Check priority directories first (same order as discoverSkills).
  // Non-root prefixes also accept depth-2 paths so the blob fast path stays
  // in sync with the on-disk walk's catalog-layout discovery.
  const priorityResults: string[] = [];
  const seen = new Set<string>();
  // Mirror of SKIP_DIRS at the top of src/skills.ts. Kept local to avoid
  // a cross-file import; if these ever drift, update both.
  const SKIP_DIRS = new Set(['node_modules', '.git', 'dist', 'build', '__pycache__']);
  const lowerSkillMdSet = new Set(filtered.map((p) => p.toLowerCase()));

  for (const priorityPrefix of PRIORITY_PREFIXES) {
    const fullPrefix = prefix + priorityPrefix;
    const isContainer = priorityPrefix !== '';

    for (const skillMd of filtered) {
      // Check if this SKILL.md is directly inside the priority dir (one level deep)
      if (!skillMd.startsWith(fullPrefix)) continue;
      const rest = skillMd.slice(fullPrefix.length);

      // Direct SKILL.md in the priority dir (e.g., "skills/SKILL.md")
      if (rest.toLowerCase() === 'skill.md') {
        if (!seen.has(skillMd)) {
          priorityResults.push(skillMd);
          seen.add(skillMd);
        }
        continue;
      }

      // SKILL.md one level deep (e.g., "skills/react-best-practices/SKILL.md")
      const parts = rest.split('/');
      if (parts.length === 2 && parts[1]!.toLowerCase() === 'skill.md') {
        if (!seen.has(skillMd)) {
          priorityResults.push(skillMd);
          seen.add(skillMd);
        }
        continue;
      }

      // SKILL.md two levels deep under a known container prefix
      // (e.g., "skills/<category>/<skill>/SKILL.md"). Skip if the parent
      // child dir already has its own SKILL.md (no descent past), or if
      // any path segment is an ignored directory.
      if (
        isContainer &&
        parts.length === 3 &&
        parts[2]!.toLowerCase() === 'skill.md' &&
        !SKIP_DIRS.has(parts[0]!) &&
        !SKIP_DIRS.has(parts[1]!)
      ) {
        const parentSkillMd = `${fullPrefix}${parts[0]}/SKILL.md`.toLowerCase();
        if (!lowerSkillMdSet.has(parentSkillMd) && !seen.has(skillMd)) {
          priorityResults.push(skillMd);
          seen.add(skillMd);
        }
      }
    }
  }

  // If we found skills in priority dirs, return those
  if (priorityResults.length > 0) return priorityResults;

  // Fallback: return all SKILL.md files found (limited to 5 levels deep)
  return filtered.filter((p) => {
    const depth = p.split('/').length;
    return depth <= 6; // 5 levels + the SKILL.md file itself
  });
}

// ─── Fetching skill content ───

/**
 * Fetch a single SKILL.md from raw.githubusercontent.com to get frontmatter.
 * Returns the raw content string, or null on failure.
 */
async function fetchSkillMdContent(
  ownerRepo: string,
  branch: string,
  skillMdPath: string
): Promise<string | null> {
  try {
    const url = `https://raw.githubusercontent.com/${ownerRepo}/${branch}/${skillMdPath}`;
    const response = await fetch(url, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT),
    });
    if (!response.ok) return null;
    return await response.text();
  } catch {
    return null;
  }
}

/**
 * Fetch a skill's full file contents from the skills.sh download API.
 * Returns the files array and content hash, or null on failure.
 */
async function fetchSkillDownload(
  source: string,
  slug: string
): Promise<SkillDownloadResponse | null> {
  try {
    const [owner, repo] = source.split('/');
    const defaultUrl = `${DOWNLOAD_BASE_URL}/api/download/${encodeURIComponent(owner!)}/${encodeURIComponent(repo!)}/${encodeURIComponent(slug)}`;
    // Self-hosted repos build their own URL; otherwise fall back to the default.
    const selfHosted = BLOB_ALLOWED_REPOS[source.toLowerCase()]?.downloadUrl(slug);
    const url = selfHosted ?? defaultUrl;
    const response = await fetch(url, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT),
    });
    if (!response.ok) return null;
    return (await response.json()) as SkillDownloadResponse;
  } catch {
    return null;
  }
}

// ─── Main entry point ───

export interface BlobInstallResult {
  skills: BlobSkill[];
  tree: RepoTree;
}

function computeSnapshotHash(files: SkillSnapshotFile[]): string {
  const hash = createHash('sha256');
  for (const file of [...files].sort((a, b) => a.path.localeCompare(b.path))) {
    hash.update(file.path);
    hash.update(file.contents);
  }
  return hash.digest('hex');
}

/**
 * Attempt to resolve skills from blob storage instead of cloning.
 *
 * Steps:
 *   1. Fetch repo tree from GitHub Trees API
 *   2. Discover SKILL.md paths from the tree
 *   3. Fetch SKILL.md content from raw.githubusercontent.com (for frontmatter/name)
 *   4. Compute slugs and fetch full snapshots from skills.sh download API
 *
 * Returns the resolved BlobSkills + tree data on success, or null on any failure
 * (the caller should fall back to git clone).
 *
 * @param ownerRepo - e.g., "vercel-labs/agent-skills"
 * @param options - subpath, skillFilter, ref, token
 */
export async function tryBlobInstall(
  ownerRepo: string,
  options: {
    subpath?: string;
    skillFilter?: string;
    ref?: string;
    getToken?: () => string | null;
    includeInternal?: boolean;
  } = {}
): Promise<BlobInstallResult | null> {
  // 1. Fetch the full repo tree
  const tree = await fetchRepoTree(ownerRepo, options.ref, options.getToken);
  if (!tree) return null;

  // 2. Discover SKILL.md paths in the tree
  let skillMdPaths = findSkillMdPaths(tree, options.subpath);
  if (skillMdPaths.length === 0) return null;

  // 3. If a skill filter is set (owner/repo@skill-name), try to narrow down
  if (options.skillFilter) {
    const filterSlug = toSkillSlug(options.skillFilter);
    const filtered = skillMdPaths.filter((p) => {
      // Match by folder name — e.g., "skills/react-best-practices/SKILL.md"
      const parts = p.split('/');
      if (parts.length < 2) return false;
      const folderName = parts[parts.length - 2]!;
      return toSkillSlug(folderName) === filterSlug;
    });
    if (filtered.length > 0) {
      skillMdPaths = filtered;
    }
    // If no match by folder name, we'll try matching by frontmatter name below
  }

  // 4. Fetch SKILL.md content from raw.githubusercontent.com in parallel
  const mdFetches = await Promise.all(
    skillMdPaths.map(async (mdPath) => {
      const content = await fetchSkillMdContent(ownerRepo, tree.branch, mdPath);
      return { mdPath, content };
    })
  );

  // Parse frontmatter to get skill names
  const parsedSkills: Array<{
    mdPath: string;
    name: string;
    description: string;
    content: string;
    slug: string;
    metadata?: Record<string, unknown>;
  }> = [];

  for (const { mdPath, content } of mdFetches) {
    if (!content) continue;

    const { data } = parseFrontmatter(content);
    if (!data.name || !data.description) continue;
    if (typeof data.name !== 'string' || typeof data.description !== 'string') continue;

    // Skip internal skills unless explicitly requested
    const isInternal = (data.metadata as Record<string, unknown>)?.internal === true;
    if (isInternal && !options.includeInternal) continue;

    const safeName = sanitizeMetadata(data.name);
    const safeDescription = sanitizeMetadata(data.description);

    parsedSkills.push({
      mdPath,
      name: safeName,
      description: safeDescription,
      content,
      slug: toSkillSlug(safeName),
      metadata: data.metadata as Record<string, unknown> | undefined,
    });
  }

  if (parsedSkills.length === 0) return null;

  // Apply skill filter by name if not already filtered by folder name
  let filteredSkills = parsedSkills;
  if (options.skillFilter) {
    const filterSlug = toSkillSlug(options.skillFilter);
    const nameFiltered = parsedSkills.filter((s) => s.slug === filterSlug);
    if (nameFiltered.length > 0) {
      filteredSkills = nameFiltered;
    }
    // If still no match, let the caller fall back to clone where
    // filterSkills() does fuzzy matching
    if (filteredSkills.length === 0) return null;
  }

  // 5. Fetch full snapshots from skills.sh download API in parallel
  const source = ownerRepo.toLowerCase();
  const downloads = await Promise.all(
    filteredSkills.map(async (skill) => {
      const download = await fetchSkillDownload(source, skill.slug);
      return { skill, download };
    })
  );

  // If ANY download failed, fall back to clone — we don't do partial blob installs
  const allSucceeded = downloads.every((d) => d.download !== null);
  if (!allSucceeded) return null;

  // 6. Convert to BlobSkill objects
  const blobSkills: BlobSkill[] = downloads.map(({ skill, download }) => {
    // Compute the folder path from the SKILL.md path (e.g., "skills/react-best-practices")
    const mdPathLower = skill.mdPath.toLowerCase();
    const folderPath = mdPathLower.endsWith('/skill.md')
      ? skill.mdPath.slice(0, -9)
      : mdPathLower === 'skill.md'
        ? ''
        : skill.mdPath.slice(0, -(1 + 'SKILL.md'.length));

    // A root-level SKILL.md means the repository root is a skill entrypoint,
    // not that the entire repository is installable skill payload. Some cached
    // snapshots for root skills contain every repo file; installing those can
    // dump thousands of unrelated files into .agents/skills/<name>. Keep root
    // skills to their SKILL.md unless/until the skill spec gains an explicit
    // include list for supporting files.
    const files = folderPath
      ? download!.files
      : download!.files.filter((file) => file.path.toLowerCase() === 'skill.md');

    return {
      name: skill.name,
      description: skill.description,
      // BlobSkills don't have a disk path — set to empty string.
      // The installer uses the files array directly.
      path: '',
      rawContent: skill.content,
      metadata: skill.metadata,
      files,
      snapshotHash:
        files.length === download!.files.length ? download!.hash : computeSnapshotHash(files),
      repoPath: skill.mdPath,
    };
  });

  return { skills: blobSkills, tree };
}

// ─── Marketplace-aware blob install ───

/** Max recursion depth for nested marketplace.json / remote plugin sources. */
const MAX_PLUGIN_DEPTH = 3;

/**
 * A SKILL.md candidate discovered via marketplace.json traversal.
 * Tracks enough context to fetch content + download later.
 */
interface MarketplaceCandidate {
  /** Path of SKILL.md within its owning repo (e.g., "skills/pdf-helper/SKILL.md") */
  mdPath: string;
  /** Parent folder name (basename of dirname) — used as skill display name */
  folderName: string;
  /** Grouping tag from marketplace plugin.name */
  pluginName: string | undefined;
  /** The repo to fetch SKILL.md content from */
  effectiveOwnerRepo: string;
  /** Branch of effectiveOwnerRepo */
  branch: string;
}

/**
 * Fetch `.claude-plugin/marketplace.json` from raw.githubusercontent.com.
 * Returns null on 404, network error, or invalid JSON.
 *
 * Uses `HEAD` as default ref since raw.githubusercontent.com only resolves
 * concrete branches — callers should pass an explicit ref (typically from
 * the Trees API branch field).
 */
export async function fetchMarketplaceJson(
  ownerRepo: string,
  ref: string = 'HEAD'
): Promise<ReturnType<typeof parseMarketplaceJson>> {
  const url = `https://raw.githubusercontent.com/${ownerRepo}/${encodeURIComponent(ref)}/.claude-plugin/marketplace.json`;
  try {
    const response = await fetch(url, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT),
    });
    if (!response.ok) return null;
    const content = await response.text();
    return parseMarketplaceJson(content);
  } catch {
    return null;
  }
}

/**
 * Strip leading "./" or "/" from a marketplace skill path so we can prefix it
 * with another directory safely. Returns empty string for the root path.
 */
function normalizePluginPath(p: string): string {
  if (p === './' || p === '/') return '';
  if (p.startsWith('./')) return p.slice(2);
  if (p.startsWith('/')) return p.slice(1);
  return p;
}

/**
 * Walk a repo's marketplace.json plus any nested remote plugin sources,
 * collecting every SKILL.md candidate. Recursion uses a BFS queue so:
 *   - depth is bounded (MAX_PLUGIN_DEPTH)
 *   - visited repos are skipped (cycle protection)
 *   - sub-plugins from remote sources flatten under the parent's plugin.name
 *
 * Returns the root repo's tree so callers can do hash lookups for the
 * lockfile. Sub-plugin skills won't have entries in that tree (they live
 * in the sub-repo); callers should fall back to other hash sources for them.
 */
async function discoverViaMarketplace(
  rootOwnerRepo: string,
  options: {
    ref?: string;
    getToken?: () => string | null;
  },
  rootTree: RepoTree
): Promise<{ candidates: MarketplaceCandidate[] } | null> {
  const candidates: MarketplaceCandidate[] = [];
  const visited = new Set<string>();
  const rootKey = rootOwnerRepo.toLowerCase();
  // Seed the queue with the already-fetched root tree to avoid a re-fetch
  const queue: Array<{
    ownerRepo: string;
    tree: RepoTree | null;
    parentPluginName: string | undefined;
    depth: number;
  }> = [{ ownerRepo: rootOwnerRepo, tree: rootTree, parentPluginName: undefined, depth: 0 }];

  while (queue.length > 0) {
    const { ownerRepo: currentRepo, tree: currentTree, parentPluginName, depth } = queue.shift()!;
    if (depth >= MAX_PLUGIN_DEPTH) continue;
    const key = currentRepo.toLowerCase();
    if (visited.has(key)) continue;
    visited.add(key);

    // Resolve the tree: use pre-fetched root tree, otherwise fetch now
    const tree = currentTree ?? (await fetchRepoTree(currentRepo, options.ref, options.getToken));
    if (!tree) continue;

    const manifest = await fetchMarketplaceJson(currentRepo, tree.branch);
    if (!manifest?.plugins?.length) continue;

    for (const plugin of manifest.plugins) {
      const source = resolvePluginSource(plugin.source, currentRepo);
      if (!source) continue;

      // Sub-plugin flattening: when this plugin is reached via a remote source
      // (parentPluginName set), all skills discovered here inherit the parent's
      // plugin.name. This makes the remote source behave as one sub-plugin
      // under the parent rather than exposing inner marketplace.json names.
      const effectivePluginName = parentPluginName ?? plugin.name;

      // Remote source: queue it as a sub-plugin and let its marketplace.json
      // be walked in a future iteration. Skills inside the sub-repo will
      // inherit effectivePluginName from the parent's plugin.name.
      if (source.kind === 'remote' && source.ownerRepo.toLowerCase() !== key) {
        queue.push({
          ownerRepo: source.ownerRepo,
          tree: null,
          parentPluginName: effectivePluginName,
          depth: depth + 1,
        });
        continue;
      }

      // Local / fallback: collect SKILL.md paths under plugin.skills (and the
      // conventional ./skills/ fallback when plugin.skills is undefined).
      //
      // pluginBase mirrors the filesystem layout: pluginRoot (from
      // marketplace.metadata) + plugin.source (when local). All plugin.skills
      // paths are resolved relative to pluginBase.
      let pluginBase = '';
      const pluginRoot = manifest.metadata?.pluginRoot;
      if (pluginRoot) {
        pluginBase = normalizePluginPath(pluginRoot);
      }
      if (source.kind === 'local') {
        const sourcePath = normalizePluginPath(source.path);
        pluginBase = pluginBase ? join(pluginBase, sourcePath) : sourcePath;
      }

      const explicitDirs = (plugin.skills ?? []).map(normalizePluginPath);
      const conventionalDir = 'skills';
      const candidateDirs: string[] = [];
      if (explicitDirs.length > 0) {
        for (const d of explicitDirs) candidateDirs.push(join(pluginBase, d));
        candidateDirs.push(join(pluginBase, conventionalDir));
      } else {
        // No explicit skills: fall back to the conventional ./skills/ dir
        // under pluginBase (which may be empty for root plugins).
        candidateDirs.push(join(pluginBase, conventionalDir));
      }

      for (const dir of candidateDirs) {
        if (!dir) continue;
        const prefix = `${dir}/`;
        for (const entry of tree.tree) {
          if (entry.type !== 'blob') continue;
          const lowerPath = entry.path.toLowerCase();
          if (!lowerPath.endsWith('/skill.md')) continue;
          if (!entry.path.startsWith(prefix)) continue;
          // Skip duplicate candidates (when explicit + conventional overlap)
          const dup = candidates.some(
            (c) => c.mdPath === entry.path && c.effectiveOwnerRepo.toLowerCase() === key
          );
          if (dup) continue;
          candidates.push({
            mdPath: entry.path,
            folderName: basename(dirname(entry.path)),
            pluginName: effectivePluginName,
            effectiveOwnerRepo: currentRepo,
            branch: tree.branch,
          });
        }
      }
    }
  }

  // Touch rootKey so eslint doesn't complain about the unused variable
  void rootKey;

  return candidates.length > 0 ? { candidates } : null;
}

/**
 * Marketplace-aware blob install path.
 *
 * 1. Walk marketplace.json (and any nested remote plugin sources) to find
 *    SKILL.md candidates, using folder name as skill name and plugin.name as
 *    pluginName (for grouping).
 * 2. Fetch each candidate's SKILL.md from raw.githubusercontent.com and
 *    parse the frontmatter (description still comes from frontmatter — only
 *    the skill NAME switches to the folder).
 * 3. Download the snapshot for each skill from the skills.sh download API.
 *
 * Returns null on any failure so the caller can fall back to clone.
 */
export async function tryMarketplaceBlobInstall(
  ownerRepo: string,
  options: {
    subpath?: string;
    skillFilter?: string;
    ref?: string;
    getToken?: () => string | null;
    includeInternal?: boolean;
  } = {}
): Promise<BlobInstallResult | null> {
  // Pre-fetch the root tree so we can return it with the BlobInstallResult
  // (used downstream by getSkillFolderHashFromTree for lockfile entries).
  const rootTree = await fetchRepoTree(ownerRepo, options.ref, options.getToken);
  if (!rootTree) return null;

  const discovery = await discoverViaMarketplace(ownerRepo, options, rootTree);
  if (!discovery) return null;

  let candidates = discovery.candidates;

  // Apply subpath filter
  if (options.subpath) {
    const prefix = options.subpath.endsWith('/') ? options.subpath : options.subpath + '/';
    candidates = candidates.filter(
      (c) => c.mdPath.startsWith(prefix) || c.mdPath === options.subpath
    );
    if (candidates.length === 0) return null;
  }

  // Apply skillFilter by folder name (matches the existing blob fast path's
  // folder-based filter so behavior stays consistent)
  if (options.skillFilter) {
    const filterSlug = toSkillSlug(options.skillFilter);
    const filtered = candidates.filter((c) => toSkillSlug(c.folderName) === filterSlug);
    if (filtered.length === 0) return null;
    candidates = filtered;
  }

  // Fetch SKILL.md content from raw.githubusercontent.com in parallel
  const contentFetches = await Promise.all(
    candidates.map(async (c) => {
      const url = `https://raw.githubusercontent.com/${c.effectiveOwnerRepo}/${encodeURIComponent(c.branch)}/${c.mdPath}`;
      try {
        const res = await fetch(url, { signal: AbortSignal.timeout(FETCH_TIMEOUT) });
        if (!res.ok) return null;
        const content = await res.text();
        return { candidate: c, content };
      } catch {
        return null;
      }
    })
  );

  const parsedSkills: Array<{
    candidate: MarketplaceCandidate;
    name: string;
    description: string;
    content: string;
    slug: string;
    metadata?: Record<string, unknown>;
  }> = [];

  for (const fetched of contentFetches) {
    if (!fetched) continue;
    const { candidate, content } = fetched;
    const { data } = parseFrontmatter(content);
    if (!data.description) continue;
    if (typeof data.description !== 'string') continue;

    // Skip internal skills unless explicitly requested
    const isInternal = (data.metadata as Record<string, unknown>)?.internal === true;
    if (isInternal && !options.includeInternal) continue;

    // Skill name = parent folder name (per product decision: marketplace
    // discovery surfaces the folder as the canonical skill identity)
    const name = candidate.folderName;

    parsedSkills.push({
      candidate,
      name,
      description: sanitizeMetadata(data.description),
      content,
      slug: toSkillSlug(name),
      metadata: data.metadata as Record<string, unknown> | undefined,
    });
  }

  if (parsedSkills.length === 0) return null;

  // Download snapshots — same per-repo download URL scheme as tryBlobInstall.
  // For nested sub-plugins, the effective ownerRepo is the sub-repo.
  const downloads = await Promise.all(
    parsedSkills.map(async (skill) => {
      const source = skill.candidate.effectiveOwnerRepo.toLowerCase();
      const download = await fetchSkillDownload(source, skill.slug);
      return { skill, download };
    })
  );

  // If ANY download failed, fall back — we don't do partial marketplace installs
  if (downloads.some((d) => d.download === null)) return null;

  // Convert to BlobSkill objects
  const blobSkills: BlobSkill[] = downloads.map(({ skill, download }) => {
    const folderPath = skill.candidate.mdPath.toLowerCase().endsWith('/skill.md')
      ? skill.candidate.mdPath.slice(0, -9)
      : skill.candidate.mdPath.toLowerCase() === 'skill.md'
        ? ''
        : skill.candidate.mdPath.slice(0, -9);

    const files = folderPath
      ? download!.files
      : download!.files.filter((file) => file.path.toLowerCase() === 'skill.md');

    return {
      name: skill.name,
      description: skill.description,
      path: '',
      rawContent: skill.content,
      metadata: skill.metadata,
      files,
      snapshotHash:
        files.length === download!.files.length ? download!.hash : computeSnapshotHash(files),
      repoPath: skill.candidate.mdPath,
      pluginName: skill.candidate.pluginName,
    };
  });

  return { skills: blobSkills, tree: rootTree };
}
