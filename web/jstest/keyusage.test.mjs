import test from 'node:test';
import assert from 'node:assert/strict';

import { heatmapIntensity, groupDailyIntoWeeks } from '../static/js/views/keyusage.js';

test('heatmapIntensity calculates 0 to 4 levels', () => {
  assert.equal(heatmapIntensity(0, 1000), 0);
  assert.equal(heatmapIntensity(-5, 1000), 0);
  assert.equal(heatmapIntensity(100, 1000), 1); // 10% -> 1
  assert.equal(heatmapIntensity(350, 1000), 2); // 35% -> 2
  assert.equal(heatmapIntensity(600, 1000), 3); // 60% -> 3
  assert.equal(heatmapIntensity(950, 1000), 4); // 95% -> 4
  assert.equal(heatmapIntensity(1200, 1000), 4); // > 100% -> 4
});

test('groupDailyIntoWeeks organizes dates into columns with 7 rows', () => {
  const points = [
    { date: '2026-10-12', tokens: 100 }, // Monday
    { date: '2026-10-13', tokens: 200 }, // Tuesday
    { date: '2026-10-14', tokens: 300 }, // Wednesday
    { date: '2026-10-15', tokens: 400 }, // Thursday
    { date: '2026-10-16', tokens: 500 }, // Friday
    { date: '2026-10-17', tokens: 600 }, // Saturday
    { date: '2026-10-18', tokens: 700 }, // Sunday
    { date: '2026-10-19', tokens: 800 }, // Monday (next week)
  ];

  const weeks = groupDailyIntoWeeks(points);
  assert.ok(weeks.length >= 2, `expected at least 2 week columns, got ${weeks.length}`);
  assert.equal(weeks[0].days.length, 7);
  assert.equal(weeks[0].days[0].tokens, 100);
});
