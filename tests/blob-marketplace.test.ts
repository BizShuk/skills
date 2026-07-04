/**
 * Tests for the marketplace-aware blob install path:
 *   - fetchMarketplaceJson(ownerRepo, ref): raw.githubusercontent.com fetch
 *   - tryMarketplaceBlobInstall(ownerRepo, options): main entry point
 *
 * Strategy: mock globalThis.fetch to assert URL patterns and return canned
 * GitHub tree / raw.githubusercontent.com / skills.sh payloads. Mirrors the
 * pattern used in tests/blob-fetch-tree-auth.test.ts.
 */

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  fetchMarketplaceJson,
  tryMarketplaceBlobInstall,
  resetRepoTreeAuthState,
} from '../src/blob.ts';

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function okText(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: { 'content-type': 'text/plain' },
  });
}

function notFound(): Response {
  return new Response('Not Found', {
    status: 404,
    headers: { 'content-type': 'text/plain' },
  });
}

const SAMPLE_SKILL_MD = `---
name: pdf-helper
description: Helper for PDF operations
---
# PDF Helper`;

describe('fetchMarketplaceJson', () => {
  let originalFetch: typeof globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('fetches the marketplace.json from raw.githubusercontent.com', async () => {
    fetchMock.mockResolvedValueOnce(okText(JSON.stringify({ plugins: [{ name: 'doc-tools' }] })));

    const result = await fetchMarketplaceJson('vercel-labs/skills', 'main');

    expect(result).not.toBeNull();
    expect(result?.plugins?.[0]?.name).toBe('doc-tools');
    const url = (fetchMock.mock.calls[0]?.[0] as string) ?? '';
    expect(url).toBe(
      'https://raw.githubusercontent.com/vercel-labs/skills/main/.claude-plugin/marketplace.json'
    );
  });

  it('uses HEAD as ref when no ref given', async () => {
    fetchMock.mockResolvedValueOnce(okText(JSON.stringify({})));

    await fetchMarketplaceJson('vercel-labs/skills');

    const url = (fetchMock.mock.calls[0]?.[0] as string) ?? '';
    expect(url).toContain('/HEAD/');
  });

  it('returns null when marketplace.json returns 404', async () => {
    fetchMock.mockResolvedValueOnce(notFound());
    expect(await fetchMarketplaceJson('foo/bar')).toBeNull();
  });

  it('returns null when marketplace.json is not valid JSON', async () => {
    fetchMock.mockResolvedValueOnce(okText('not json at all'));
    expect(await fetchMarketplaceJson('foo/bar')).toBeNull();
  });

  it('returns null when marketplace.json is a JSON array', async () => {
    fetchMock.mockResolvedValueOnce(okJson([1, 2, 3]));
    expect(await fetchMarketplaceJson('foo/bar')).toBeNull();
  });

  it('returns null when marketplace.json is empty', async () => {
    fetchMock.mockResolvedValueOnce(okText(''));
    expect(await fetchMarketplaceJson('foo/bar')).toBeNull();
  });
});

describe('tryMarketplaceBlobInstall', () => {
  let originalFetch: typeof globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    resetRepoTreeAuthState();
    originalFetch = globalThis.fetch;
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  // Helper: assemble canned responses for tree + marketplace.json + raw SKILL.md + download
  function mockInstall(opts: {
    treeEntries: Array<{ path: string; type: 'blob' | 'tree'; sha?: string }>;
    marketplaceJson: unknown;
    skillContents?: Record<string, string>;
    downloadResponse?: { files: Array<{ path: string; contents: string }>; hash: string };
  }) {
    const skillMd = opts.skillContents ?? {};
    const download = opts.downloadResponse ?? { files: [], hash: 'abc123' };

    fetchMock.mockImplementation(async (url: string) => {
      // Trees API
      if (url.includes('api.github.com/repos/') && url.includes('/git/trees/')) {
        return okJson({ sha: 'main-sha', tree: opts.treeEntries });
      }
      // Marketplace raw
      if (
        url.includes('raw.githubusercontent.com/') &&
        url.endsWith('/.claude-plugin/marketplace.json')
      ) {
        if (opts.marketplaceJson === null) return notFound();
        return okText(JSON.stringify(opts.marketplaceJson));
      }
      // SKILL.md raw content
      if (url.includes('raw.githubusercontent.com/') && url.endsWith('/SKILL.md')) {
        // URL shape: raw.githubusercontent.com/{owner}/{repo}/{ref}/{path...}
        const path = url.split('raw.githubusercontent.com/')[1]!.split('/').slice(3).join('/');
        if (skillMd[path]) return okText(skillMd[path]);
        return notFound();
      }
      // Download API
      if (url.includes('/api/download/')) {
        return okJson(download);
      }
      return notFound();
    });
  }

  it('returns null when no marketplace.json', async () => {
    mockInstall({
      treeEntries: [{ path: 'skills/a/SKILL.md', type: 'blob' }],
      marketplaceJson: null,
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).toBeNull();
  });

  it('returns null when marketplace.json has no plugins array', async () => {
    mockInstall({
      treeEntries: [{ path: 'skills/a/SKILL.md', type: 'blob' }],
      marketplaceJson: { metadata: {} },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).toBeNull();
  });

  it('returns null when marketplace.json has empty plugins array', async () => {
    mockInstall({
      treeEntries: [{ path: 'skills/a/SKILL.md', type: 'blob' }],
      marketplaceJson: { plugins: [] },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).toBeNull();
  });

  it('discovers SKILL.md via plugin.skills path and uses folder name as skill name', async () => {
    mockInstall({
      treeEntries: [
        { path: 'skills/pdf-helper/SKILL.md', type: 'blob' },
        { path: 'skills/pdf-helper/main.py', type: 'blob' },
      ],
      marketplaceJson: {
        plugins: [
          {
            name: 'document-skills',
            skills: ['./skills/pdf-helper'],
          },
        ],
      },
      skillContents: {
        'skills/pdf-helper/SKILL.md': SAMPLE_SKILL_MD,
      },
      downloadResponse: {
        files: [
          { path: 'SKILL.md', contents: SAMPLE_SKILL_MD },
          { path: 'main.py', contents: 'print("hello")' },
        ],
        hash: 'snap-hash',
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');

    expect(result).not.toBeNull();
    const skills = result!.skills;
    expect(skills).toHaveLength(1);
    expect(skills[0]!.name).toBe('pdf-helper'); // folder name, not frontmatter "pdf-helper" anyway but proves the path
    expect(skills[0]!.pluginName).toBe('document-skills');
    expect(skills[0]!.repoPath).toBe('skills/pdf-helper/SKILL.md');
  });

  it('sets pluginName from plugin.name for multiple skills under one plugin', async () => {
    mockInstall({
      treeEntries: [
        { path: 'skills/pdf-helper/SKILL.md', type: 'blob' },
        { path: 'skills/docx-tool/SKILL.md', type: 'blob' },
      ],
      marketplaceJson: {
        plugins: [
          {
            name: 'document-skills',
            skills: ['./skills/pdf-helper', './skills/docx-tool'],
          },
        ],
      },
      skillContents: {
        'skills/pdf-helper/SKILL.md': SAMPLE_SKILL_MD,
        'skills/docx-tool/SKILL.md': SAMPLE_SKILL_MD,
      },
      downloadResponse: {
        files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }],
        hash: 'snap',
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).not.toBeNull();
    const pluginNames = result!.skills.map((s) => s.pluginName);
    expect(new Set(pluginNames)).toEqual(new Set(['document-skills']));
    const skillNames = result!.skills.map((s) => s.name).sort();
    expect(skillNames).toEqual(['docx-tool', 'pdf-helper']);
  });

  it('falls back to conventional ./skills when plugin.skills is undefined', async () => {
    mockInstall({
      treeEntries: [{ path: 'skills/auto-found/SKILL.md', type: 'blob' }],
      marketplaceJson: {
        plugins: [
          { name: 'auto-plugin' },
          // no skills[] defined → should look in ./skills/ by convention
        ],
      },
      skillContents: {
        'skills/auto-found/SKILL.md': SAMPLE_SKILL_MD,
      },
      downloadResponse: {
        files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }],
        hash: 'snap',
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).not.toBeNull();
    expect(result!.skills).toHaveLength(1);
    expect(result!.skills[0]!.pluginName).toBe('auto-plugin');
    expect(result!.skills[0]!.name).toBe('auto-found');
  });

  it('handles multiple plugins independently', async () => {
    mockInstall({
      treeEntries: [
        { path: 'plugins/pdf/skills/pdf-helper/SKILL.md', type: 'blob' },
        { path: 'plugins/email/skills/mailer/SKILL.md', type: 'blob' },
      ],
      marketplaceJson: {
        plugins: [
          {
            name: 'pdf-plugin',
            source: './plugins/pdf',
            skills: ['./skills/pdf-helper'],
          },
          {
            name: 'email-plugin',
            source: './plugins/email',
            skills: ['./skills/mailer'],
          },
        ],
      },
      skillContents: {
        'plugins/pdf/skills/pdf-helper/SKILL.md': SAMPLE_SKILL_MD,
        'plugins/email/skills/mailer/SKILL.md': SAMPLE_SKILL_MD,
      },
      downloadResponse: {
        files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }],
        hash: 'snap',
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).not.toBeNull();
    expect(result!.skills).toHaveLength(2);

    const byName = Object.fromEntries(result!.skills.map((s) => [s.name, s.pluginName]));
    expect(byName).toEqual({
      'pdf-helper': 'pdf-plugin',
      mailer: 'email-plugin',
    });
  });

  it('returns null when no plugins have resolvable source', async () => {
    // Both plugins reference invalid local sources
    mockInstall({
      treeEntries: [{ path: 'skills/a/SKILL.md', type: 'blob' }],
      marketplaceJson: {
        plugins: [
          { name: 'p1', source: 'no-prefix' }, // invalid per spec
          { name: 'p2', source: { repo: 'no-slash' } }, // invalid repo format
        ],
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).toBeNull();
  });

  it('recurses into remote source as a sub-plugin and flattens under parent plugin.name', async () => {
    // Outer marketplace has a plugin whose source points to a remote repo.
    // The remote repo has its own marketplace with a sub-plugin.
    // Skills in the sub-plugin should inherit the parent plugin.name.
    fetchMock.mockImplementation(async (url: string) => {
      // Outer trees API
      if (url === 'https://api.github.com/repos/outer/repo/git/trees/HEAD?recursive=1') {
        return okJson({
          sha: 'outer-sha',
          tree: [{ path: '.claude-plugin/marketplace.json', type: 'blob', sha: 'mp' }],
        });
      }
      // Sub-repo trees API
      if (url === 'https://api.github.com/repos/sub-org/sub-repo/git/trees/HEAD?recursive=1') {
        return okJson({
          sha: 'sub-sha',
          tree: [{ path: 'skills/sub-skill/SKILL.md', type: 'blob', sha: 'ss' }],
        });
      }
      // Outer marketplace
      if (url.endsWith('/outer/repo/HEAD/.claude-plugin/marketplace.json')) {
        return okText(
          JSON.stringify({
            plugins: [
              {
                name: 'parent-bundle',
                source: 'sub-org/sub-repo',
                skills: ['./skills/sub-skill'],
              },
            ],
          })
        );
      }
      // Sub-repo marketplace
      if (url.endsWith('/sub-repo/HEAD/.claude-plugin/marketplace.json')) {
        return okText(
          JSON.stringify({
            plugins: [
              {
                name: 'inner-plugin',
                skills: ['./skills/sub-skill'],
              },
            ],
          })
        );
      }
      // SKILL.md in sub-repo
      if (url.endsWith('/sub-repo/HEAD/skills/sub-skill/SKILL.md')) {
        return okText(SAMPLE_SKILL_MD);
      }
      // Download (sub-repo)
      if (url.includes('/api/download/sub-org/sub-repo/sub-skill')) {
        return okJson({
          files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }],
          hash: 'snap',
        });
      }
      return notFound();
    });

    const result = await tryMarketplaceBlobInstall('outer/repo');

    expect(result).not.toBeNull();
    expect(result!.skills).toHaveLength(1);
    // Inherited from the parent plugin (not the inner marketplace's name)
    expect(result!.skills[0]!.pluginName).toBe('parent-bundle');
    expect(result!.skills[0]!.name).toBe('sub-skill');
    expect(result!.skills[0]!.repoPath).toBe('skills/sub-skill/SKILL.md');
  });

  it('skips cycles (a plugin whose source points back to the root)', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === 'https://api.github.com/repos/foo/bar/git/trees/HEAD?recursive=1') {
        return okJson({ sha: 'sha', tree: [{ path: 'skills/a/SKILL.md', type: 'blob' }] });
      }
      if (url.endsWith('/foo/bar/HEAD/.claude-plugin/marketplace.json')) {
        return okText(
          JSON.stringify({
            plugins: [
              // Self-reference: would loop forever without cycle protection
              { name: 'self', source: 'foo/bar', skills: ['./skills/a'] },
            ],
          })
        );
      }
      if (url.endsWith('/foo/bar/HEAD/skills/a/SKILL.md')) {
        return okText(SAMPLE_SKILL_MD);
      }
      if (url.includes('/api/download/foo/bar/a')) {
        return okJson({ files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }], hash: 'h' });
      }
      return notFound();
    });

    const result = await tryMarketplaceBlobInstall('foo/bar');
    expect(result).not.toBeNull();
    expect(result!.skills).toHaveLength(1);
    expect(result!.skills[0]!.name).toBe('a');
  });

  it('uses skillFilter to narrow the result set', async () => {
    mockInstall({
      treeEntries: [
        { path: 'skills/pdf-helper/SKILL.md', type: 'blob' },
        { path: 'skills/mailer/SKILL.md', type: 'blob' },
      ],
      marketplaceJson: {
        plugins: [{ name: 'bundle', skills: ['./skills/pdf-helper', './skills/mailer'] }],
      },
      skillContents: {
        'skills/pdf-helper/SKILL.md': SAMPLE_SKILL_MD,
        'skills/mailer/SKILL.md': SAMPLE_SKILL_MD,
      },
      downloadResponse: {
        files: [{ path: 'SKILL.md', contents: SAMPLE_SKILL_MD }],
        hash: 'snap',
      },
    });

    const result = await tryMarketplaceBlobInstall('foo/bar', { skillFilter: 'pdf-helper' });
    expect(result).not.toBeNull();
    expect(result!.skills.map((s) => s.name)).toEqual(['pdf-helper']);
  });
});
