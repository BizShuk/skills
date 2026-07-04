/**
 * Coverage for the six marketplace plugin source-type configurations
 * defined by the Claude Code plugin marketplace schema:
 *
 *   1. github        + repo
 *   2. github        + repo + ref + sha
 *   3. url           + url
 *   4. url           + url + ref + sha
 *   5. git-subdir    + url + path
 *   6. git-subdir    + url + path + ref + sha
 *
 * These tests pin the contract for both `parseMarketplaceJson` (round-trip
 * fidelity) and `resolvePluginSource` (actionable resolution). The latter
 * requires the marketplace walker to honor `ref`, `sha`, `url`, and `path`,
 * not just `ownerRepo` — current implementation only handles the simplest
 * { repo } object form, so cases 2–6 surface the gaps.
 */
import { describe, it, expect } from 'vitest';
import { parseMarketplaceJson, resolvePluginSource } from './plugin-manifest.ts';

const SHA = 'a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0';

// Each row is [label, source, expected]. Using array-of-arrays so vitest's
// it.each spreads correctly into 3 positional args.
const FIXTURES: Array<[string, Record<string, unknown>, Record<string, unknown>]> = [
  [
    '1. github + repo only',
    { source: 'github', repo: 'owner/plugin-repo' },
    { kind: 'remote', ownerRepo: 'owner/plugin-repo' },
  ],
  [
    '2. github + repo + ref + sha',
    { source: 'github', repo: 'owner/plugin-repo', ref: 'v2.0.0', sha: SHA },
    { kind: 'remote', ownerRepo: 'owner/plugin-repo', ref: 'v2.0.0', sha: SHA },
  ],
  [
    '3. url + url only',
    { source: 'url', url: 'https://gitlab.com/team/plugin.git' },
    { kind: 'remote', ownerRepo: 'team/plugin', url: 'https://gitlab.com/team/plugin.git' },
  ],
  [
    '4. url + url + ref + sha',
    { source: 'url', url: 'https://gitlab.com/team/plugin.git', ref: 'main', sha: SHA },
    {
      kind: 'remote',
      ownerRepo: 'team/plugin',
      url: 'https://gitlab.com/team/plugin.git',
      ref: 'main',
      sha: SHA,
    },
  ],
  [
    '5. git-subdir + url + path',
    {
      source: 'git-subdir',
      url: 'https://github.com/acme-corp/monorepo.git',
      path: 'tools/claude-plugin',
    },
    {
      kind: 'remote',
      ownerRepo: 'acme-corp/monorepo',
      url: 'https://github.com/acme-corp/monorepo.git',
      subdir: 'tools/claude-plugin',
    },
  ],
  [
    '6. git-subdir + url + path + ref + sha',
    {
      source: 'git-subdir',
      url: 'https://github.com/acme-corp/monorepo.git',
      path: 'tools/claude-plugin',
      ref: 'v2.0.0',
      sha: SHA,
    },
    {
      kind: 'remote',
      ownerRepo: 'acme-corp/monorepo',
      url: 'https://github.com/acme-corp/monorepo.git',
      subdir: 'tools/claude-plugin',
      ref: 'v2.0.0',
      sha: SHA,
    },
  ],
];

describe('marketplace plugin source types — parseMarketplaceJson round-trip', () => {
  it.each(FIXTURES)('preserves source object faithfully (%s)', (_label, source) => {
    const json = JSON.stringify({
      plugins: [{ name: 'under-test', source }],
    });
    const parsed = parseMarketplaceJson(json);
    expect(parsed).not.toBeNull();
    expect(parsed!.plugins).toHaveLength(1);
    expect(parsed!.plugins![0]!.source).toEqual(source);
  });
});

describe('marketplace plugin source types — resolvePluginSource', () => {
  it.each(FIXTURES)('resolves to expected actionable form (%s)', (_label, source, expected) => {
    const r = resolvePluginSource(source as Parameters<typeof resolvePluginSource>[0], 'host/main');
    expect(r).toEqual(expected);
  });
});
