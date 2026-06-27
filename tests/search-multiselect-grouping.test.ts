import { describe, expect, it } from 'vitest';
import { buildDisplayRows } from '../src/prompts/search-multiselect.ts';

describe('buildDisplayRows grouping logic', () => {
  it('handles empty items', () => {
    const rows = buildDisplayRows([]);
    expect(rows).toEqual([]);
  });

  it('passes through ungrouped items without headers', () => {
    const items = [
      { value: 'a', label: 'Item A' },
      { value: 'b', label: 'Item B' },
    ];
    const rows = buildDisplayRows(items);
    expect(rows).toEqual([
      { type: 'item', groupName: '', item: items[0] },
      { type: 'item', groupName: '', item: items[1] },
    ]);
  });

  it('inserts group headers when groups change', () => {
    const items = [
      { value: 'a', label: 'Item A', group: 'Group 1' },
      { value: 'b', label: 'Item B', group: 'Group 1' },
      { value: 'c', label: 'Item C', group: 'Group 2' },
    ];
    const rows = buildDisplayRows(items);
    expect(rows).toEqual([
      { type: 'group', groupName: 'Group 1' },
      { type: 'item', groupName: 'Group 1', item: items[0] },
      { type: 'item', groupName: 'Group 1', item: items[1] },
      { type: 'group', groupName: 'Group 2' },
      { type: 'item', groupName: 'Group 2', item: items[2] },
    ]);
  });
});
