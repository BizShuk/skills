import { discoverSkills } from './src/skills.ts';

(async () => {
  const skills = await discoverSkills('/Users/shuk/projects/cc-plugin');
  console.log('Total skills:', skills.length);
  const withPlugin = skills.filter(s => s.pluginName);
  const without = skills.filter(s => !s.pluginName);
  console.log('With pluginName:', withPlugin.length);
  console.log('Without pluginName:', without.length);
  
  const pluginNames = new Set(withPlugin.map(s => s.pluginName));
  console.log('Unique plugin names:', Array.from(pluginNames).sort());
  
  console.log('\nSamples with pluginName:');
  withPlugin.slice(0, 10).forEach(s => console.log(`  ${s.name} → ${s.pluginName}`));
})();
