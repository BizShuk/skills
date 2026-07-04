import { discoverSkills } from './src/skills.ts';

(async () => {
  const skills = await discoverSkills('/Users/shuk/projects/tmp/skills');
  console.log('Total skills:', skills.length);
  const withPlugin = skills.filter(s => s.pluginName);
  const without = skills.filter(s => !s.pluginName);
  console.log('With pluginName:', withPlugin.length);
  console.log('Without pluginName:', without.length);
  
  const pluginNames = new Set(withPlugin.map(s => s.pluginName));
  console.log('Unique plugin names:', Array.from(pluginNames));
  
  console.log('\nFirst 5 ungrouped:');
  without.slice(0, 5).forEach(s => console.log('  -', s.name, 'path=', s.path));
})();
