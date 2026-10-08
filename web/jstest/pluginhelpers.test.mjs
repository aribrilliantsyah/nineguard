import test from 'node:test';
import assert from 'node:assert/strict';
import { scopeLabel, scopeSubLabel, formatInheritText, sortPipeline, warningBannerText } from '../static/js/pluginhelpers.js';

test('scopeLabel conforms to spec §9.2', () => {
  assert.equal(scopeLabel('global'), 'All keys, all models');
  assert.equal(scopeLabel('group', 'Coding'), 'Models in "Coding"');
  assert.equal(scopeLabel('key', 'Cursor IDE'), 'Key "Cursor IDE"');
});

test('scopeSubLabel conforms to spec §9.2', () => {
  assert.equal(scopeSubLabel('global'), 'Default for every request. Groups and keys below can override it.');
  assert.equal(scopeSubLabel('group', { modelsCount: 4, keysCount: 2 }), '4 models · any key · linked to 2 keys for access');
  assert.equal(scopeSubLabel('group', { modelsCount: 2, keysCount: 0 }), '2 models · any key · plugin-only group');
  assert.equal(scopeSubLabel('key'), 'Any model this key uses');
});

test('formatInheritText renders next broader scope', () => {
  assert.equal(formatInheritText('key'), 'Inherit (use group or All keys, all models)');
  assert.equal(formatInheritText('group'), 'Inherit (use All keys, all models)');
});

test('sortPipeline sorts by pipeline_order ASC', () => {
  const list = [
    { id: 'caveman', pipeline_order: 30 },
    { id: 'headroom', pipeline_order: 10 },
    { id: 'ponytail', pipeline_order: 20 },
  ];
  const sorted = sortPipeline(list);
  assert.deepEqual(sorted.map((p) => p.id), ['headroom', 'ponytail', 'caveman']);
});

test('warningBannerText pluralization', () => {
  assert.equal(warningBannerText(1), '1 overlap warning active');
  assert.equal(warningBannerText(3), '3 overlap warnings active');
});
