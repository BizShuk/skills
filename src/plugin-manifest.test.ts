/**
 * Tests for the parse helpers in src/plugin-manifest.ts:
 *   - parseMarketplaceJson(content): strict parser with no fs dependency
 *   - resolvePluginSource(source, fallback): maps plugin.source to one of
 *     { kind: 'local' } | { kind: 'remote' } | { kind: 'fallback' } | null
 *
 * These helpers back the marketplace-aware blob install path. The existing
 * filesystem functions (getPluginSkillPaths / getPluginGroupings) are covered
 * by tests/plugin-manifest-discovery.test.ts; we only add tests for the new
 * pure-function surface here.
 */
import { describe, it, expect } from 'vitest';
import { parseMarketplaceJson, resolvePluginSource } from './plugin-manifest.ts';

describe('parseMarketplaceJson', () => {
  it('parses a minimal valid marketplace', () => {
    const json = JSON.stringify({
      plugins: [{ name: 'p', skills: ['./skills/a'] }],
    });
    const parsed = parseMarketplaceJson(json);
    expect(parsed).not.toBeNull();
    expect(parsed!.plugins).toHaveLength(1);
    expect(parsed!.plugins![0]!.name).toBe('p');
  });

  it('parses with metadata.pluginRoot', () => {
    const json = JSON.stringify({
      metadata: { pluginRoot: './plugins' },
      plugins: [{ name: 'p', source: './p', skills: ['./skills/a'] }],
    });
    const parsed = parseMarketplaceJson(json);
    expect(parsed?.metadata?.pluginRoot).toBe('./plugins');
  });

  it('returns null for invalid JSON', () => {
    expect(parseMarketplaceJson('not json')).toBeNull();
    expect(parseMarketplaceJson('{ invalid }')).toBeNull();
    expect(parseMarketplaceJson('')).toBeNull();
  });

  it('returns null for non-object JSON', () => {
    expect(parseMarketplaceJson('null')).toBeNull();
    expect(parseMarketplaceJson('"a string"')).toBeNull();
    expect(parseMarketplaceJson('42')).toBeNull();
    expect(parseMarketplaceJson('[]')).toBeNull();
  });

  it('returns an empty manifest for {}', () => {
    const parsed = parseMarketplaceJson('{}');
    expect(parsed).not.toBeNull();
    expect(parsed!.plugins).toBeUndefined();
  });

  it('preserves the object form of source for resolvePluginSource consumers', () => {
    const json = JSON.stringify({
      plugins: [{ name: 'remote', source: { source: 'github', repo: 'foo/bar' } }],
    });
    const parsed = parseMarketplaceJson(json);
    const plugin = parsed!.plugins![0]!;
    expect(typeof plugin.source).toBe('object');
    expect((plugin.source as { repo: string }).repo).toBe('foo/bar');
  });
});

describe('resolvePluginSource', () => {
  it('returns fallback when source is undefined', () => {
    const r = resolvePluginSource(undefined, 'vercel-labs/skills');
    expect(r).toEqual({ kind: 'fallback', ownerRepo: 'vercel-labs/skills' });
  });

  it('treats ./ relative paths as local', () => {
    const r = resolvePluginSource('./plugins/foo', 'vercel-labs/skills');
    expect(r).toEqual({ kind: 'local', path: './plugins/foo' });
  });

  it('treats ../ relative paths as local', () => {
    const r = resolvePluginSource('../shared', 'vercel-labs/skills');
    expect(r).toEqual({ kind: 'local', path: '../shared' });
  });

  it('resolves owner/repo shorthand to remote github', () => {
    const r = resolvePluginSource('vercel-labs/agent-skills', 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'vercel-labs/agent-skills' });
  });

  it('resolves github:owner/repo prefix to remote', () => {
    const r = resolvePluginSource('github:vercel-labs/agent-skills', 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'vercel-labs/agent-skills' });
  });

  it('resolves https URL to remote', () => {
    const r = resolvePluginSource('https://github.com/vercel-labs/agent-skills', 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'vercel-labs/agent-skills' });
  });

  it('resolves git@ SSH URL to remote', () => {
    const r = resolvePluginSource('git@github.com:vercel-labs/agent-skills.git', 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'vercel-labs/agent-skills' });
  });

  it('resolves { source: github, repo } object form to remote', () => {
    const r = resolvePluginSource({ source: 'github', repo: 'foo/bar' }, 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'foo/bar' });
  });

  it('resolves { repo } only object form to remote', () => {
    const r = resolvePluginSource({ repo: 'foo/bar' }, 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'foo/bar' });
  });

  it('lowercases owner/repo for consistency', () => {
    const r = resolvePluginSource('Foo/Bar', 'host/main');
    expect(r).toEqual({ kind: 'remote', ownerRepo: 'foo/bar' });
  });

  it('returns null for unparseable strings', () => {
    expect(resolvePluginSource('just-a-word', 'host/main')).toBeNull();
    expect(resolvePluginSource('', 'host/main')).toBeNull();
  });

  it('returns null for object with invalid repo (no slash)', () => {
    expect(resolvePluginSource({ source: 'github', repo: 'no-slash' }, 'host/main')).toBeNull();
    expect(resolvePluginSource({ source: 'github' }, 'host/main')).toBeNull();
  });

  it('returns null for non-string non-object inputs', () => {
    // @ts-expect-error - intentionally wrong type for runtime safety check
    expect(resolvePluginSource(123, 'host/main')).toBeNull();
    // @ts-expect-error - intentionally wrong type for runtime safety check
    expect(resolvePluginSource(null, 'host/main')).toBeNull();
  });
});
