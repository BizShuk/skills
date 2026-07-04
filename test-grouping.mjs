import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { getPluginGroupings } from './src/plugin-manifest.ts';
import { discoverSkills } from './src/skills.ts';

(async () => {
  const baseDir = join(tmpdir(), `plugin-test-${Date.now()}`);
  mkdirSync(baseDir, { recursive: true });
  
  mkdirSync(join(baseDir, '.claude-plugin'), { recursive: true });
  writeFileSync(
    join(baseDir, '.claude-plugin/plugin.json'),
    JSON.stringify({ name: 'my-plugin' })
  );
  
  mkdirSync(join(baseDir, 'skills/foo'), { recursive: true });
  writeFileSync(join(baseDir, 'skills/foo/SKILL.md'), `---
name: foo
description: Test
---
# Foo`);
  
  console.log('Base dir:', baseDir);
  const groupings = await getPluginGroupings(baseDir);
  console.log('Groupings count:', groupings.size);
  console.log('Groupings entries:', Array.from(groupings.entries()));
  
  const skills = await discoverSkills(baseDir);
  console.log('Discovered skills:', skills.length);
  skills.forEach(s => console.log(`  - ${s.name} | pluginName=${s.pluginName ?? 'NONE'}`));
  
  rmSync(baseDir, { recursive: true, force: true });
})();
