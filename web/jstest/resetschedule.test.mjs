import test from 'node:test';
import assert from 'node:assert/strict';
import {
  neededFields, usedPeriods, scheduleChoice, buildSchedulePayload, validateSchedule,
  scheduleSourceLabel, describeSchedule, defaultSchedule, nextResetText, WEEKDAYS,
} from '../static/js/resetschedule.js';

const jkt = { timezone: 'Asia/Jakarta', time: '14:00', weekday: 3, day: 20 };

test('usedPeriods lists the periods of enforced quotas', () => {
  assert.deepEqual(usedPeriods({ mode: 'key', keyPeriod: 'monthly', rows: [] }), ['monthly']);
  assert.deepEqual(usedPeriods({ mode: 'key', keyPeriod: 'none', rows: [] }), []);
  assert.deepEqual(usedPeriods({ mode: 'key', keyPeriod: 'total', rows: [] }), ['total']);
  // model mode ignores the Key Quota period; rows decide, de-duplicated, stable order
  assert.deepEqual(usedPeriods({ mode: 'model', keyPeriod: 'monthly', rows: [{ period: 'weekly' }, { period: 'daily' }, { period: 'weekly' }] }), ['daily', 'weekly']);
  assert.deepEqual(usedPeriods({ mode: 'model', keyPeriod: 'daily', rows: [] }), []);
  // key mode ignores stored model rows
  assert.deepEqual(usedPeriods({ mode: 'key', keyPeriod: 'daily', rows: [{ period: 'monthly' }] }), ['daily']);
});

test('neededFields: which inputs the periods in use require', () => {
  assert.equal(neededFields(['daily']).show, true);
  assert.deepEqual(neededFields(['daily']), { show: true, time: true, weekday: false, day: false });
  assert.deepEqual(neededFields(['weekly']), { show: true, time: true, weekday: true, day: false });
  assert.deepEqual(neededFields(['monthly']), { show: true, time: true, weekday: false, day: true });
  assert.deepEqual(neededFields(['daily', 'weekly', 'monthly']), { show: true, time: true, weekday: true, day: true });
});

test('neededFields: block hidden when nothing resets', () => {
  for (const periods of [[], ['total'], ['none'], ['total', 'none']]) {
    assert.deepEqual(neededFields(periods), { show: false, time: false, weekday: false, day: false }, JSON.stringify(periods));
  }
  // total next to a real period does not hide it
  assert.equal(neededFields(['total', 'daily']).show, true);
});

test('scheduleChoice maps a key to follow / custom', () => {
  assert.equal(scheduleChoice(null), 'follow');
  assert.equal(scheduleChoice(undefined), 'follow');
  assert.equal(scheduleChoice(jkt), 'custom');
});

test('buildSchedulePayload: follow sends null, custom sends the full object', () => {
  assert.equal(buildSchedulePayload('follow', jkt), null);
  assert.deepEqual(buildSchedulePayload('custom', jkt), jkt);
  // numbers coming from <select>/<input> as strings are normalised
  assert.deepEqual(
    buildSchedulePayload('custom', { timezone: 'UTC', time: '06:30', weekday: '1', day: '15' }),
    { timezone: 'UTC', time: '06:30', weekday: 1, day: 15 });
  // a custom UTC schedule is a real object, not null: it pins the key to UTC
  assert.deepEqual(buildSchedulePayload('custom', { timezone: 'UTC', time: '00:00', weekday: 1, day: 1 }),
    { timezone: 'UTC', time: '00:00', weekday: 1, day: 1 });
});

test('validateSchedule mirrors the server rules', () => {
  assert.equal(validateSchedule(jkt), '');
  assert.match(validateSchedule({ ...jkt, timezone: '' }), /timezone/i);
  assert.match(validateSchedule({ ...jkt, timezone: 'Local' }), /timezone/i);
  assert.match(validateSchedule({ ...jkt, timezone: 'x'.repeat(65) }), /timezone/i);
  assert.match(validateSchedule({ ...jkt, time: '24:00' }), /time/i);
  assert.match(validateSchedule({ ...jkt, time: '9:5' }), /time/i);
  assert.match(validateSchedule({ ...jkt, time: '' }), /time/i);
  assert.match(validateSchedule({ ...jkt, time: '10:60' }), /time/i);
  assert.match(validateSchedule({ ...jkt, weekday: 0 }), /weekday/i);
  assert.match(validateSchedule({ ...jkt, weekday: 8 }), /weekday/i);
  assert.match(validateSchedule({ ...jkt, day: 0 }), /day/i);
  assert.match(validateSchedule({ ...jkt, day: 32 }), /day/i);
  assert.match(validateSchedule({ ...jkt, day: 'x' }), /day/i);
  assert.equal(validateSchedule({ ...jkt, time: '00:00' }), '');
  assert.equal(validateSchedule({ ...jkt, time: '23:59', day: 31, weekday: 7 }), '');
});

test('scheduleSourceLabel explains where the schedule comes from', () => {
  assert.equal(scheduleSourceLabel('key', jkt), 'Custom schedule');
  assert.match(scheduleSourceLabel('global', jkt), /global/i);
  assert.match(scheduleSourceLabel('global', jkt), /Asia\/Jakarta/);
  assert.match(scheduleSourceLabel('global', jkt), /14:00/);
  assert.match(scheduleSourceLabel('default', null), /UTC/);
  assert.match(scheduleSourceLabel('default', null), /no global/i);
});

test('describeSchedule renders only the fields the periods need', () => {
  assert.equal(describeSchedule(jkt, ['daily']), 'Asia/Jakarta, 14:00');
  assert.equal(describeSchedule(jkt, ['weekly']), 'Asia/Jakarta, Wednesday 14:00');
  assert.equal(describeSchedule(jkt, ['monthly']), 'Asia/Jakarta, day 20 at 14:00');
  assert.equal(describeSchedule(jkt, ['weekly', 'monthly']), 'Asia/Jakarta, Wednesday, day 20 at 14:00');
  assert.equal(describeSchedule(null, ['daily']), 'UTC, 00:00');
  assert.equal(describeSchedule(null, ['weekly']), 'UTC, Monday 00:00');
  assert.equal(describeSchedule(null, ['monthly']), 'UTC, day 1 at 00:00');
});

test('defaultSchedule prefers the global schedule, then the viewer timezone', () => {
  assert.deepEqual(defaultSchedule(jkt, 'Europe/Berlin'), jkt);
  assert.deepEqual(defaultSchedule(null, 'Europe/Berlin'), { timezone: 'Europe/Berlin', time: '00:00', weekday: 1, day: 1 });
  assert.equal(defaultSchedule(null, '').timezone, 'UTC');
  assert.equal(defaultSchedule(null, undefined).timezone, 'UTC');
  // returns a copy, never the global object itself
  const g = { ...jkt };
  const d = defaultSchedule(g, 'UTC');
  d.time = '01:00';
  assert.equal(g.time, '14:00');
});

test('nextResetText formats an absolute instant in the schedule timezone', () => {
  const at = '2026-11-02T07:00:00Z'; // 14:00 in Jakarta
  assert.equal(nextResetText(at, 'Asia/Jakarta'), 'Mon, 02 Nov 2026 14:00 WIB');
  assert.equal(nextResetText(at, 'UTC'), 'Mon, 02 Nov 2026 07:00 UTC');
  assert.equal(nextResetText('', 'UTC'), '');
  assert.equal(nextResetText('garbage', 'UTC'), '');
  // a zone Intl cannot resolve falls back to UTC rather than throwing
  assert.match(nextResetText(at, 'Not/AZone'), /07:00 UTC$/);
});

test('WEEKDAYS is Monday-first with ISO numbers', () => {
  assert.deepEqual(WEEKDAYS.map(([n]) => n), [1, 2, 3, 4, 5, 6, 7]);
  assert.equal(WEEKDAYS[0][1], 'Monday');
  assert.equal(WEEKDAYS[6][1], 'Sunday');
});
