import test from 'node:test';
import assert from 'node:assert/strict';
import { remainingLabel, observationLabel, freshPercent, sdkMeter, splitPoolBuckets } from '../static/js/upstreamtelemetry.js';

test('SDK remaining distinguishes measured zero from absent values', () => {
  assert.equal(remainingLabel({ unit: 'percent', remaining_percent: 0 }), '0% left');
  assert.equal(remainingLabel({ unit: 'percent' }), 'Unknown');
  assert.equal(remainingLabel({ unit: 'percent', limit_reached: true }), 'Limit reached');
  assert.equal(remainingLabel({ unit: 'percent', remaining_percent: 47 }), '47% left');
});
test('SDK fresh snapshot excludes stale, partial and past reset measurements', () => {
  const now = Date.parse('2026-10-26T12:00:00Z');
  const b = { unit: 'percent', observation_state: 'observed', observed_at: '2026-10-26T11:59:00Z', remaining_percent: 5 };
  assert.equal(freshPercent(b, now), true);
  assert.equal(freshPercent({ ...b, incomplete: true }, now), false);
  assert.equal(freshPercent({ ...b, reset_at: '2026-10-26T11:59:50Z' }, now), false);
  assert.equal(freshPercent({ ...b, observed_at: '2026-10-26T11:00:00Z' }, now), false);
  assert.equal(observationLabel(null, 'waiting'), 'Waiting for upstream telemetry');
  assert.equal(observationLabel('invalid', 'waiting'), 'Waiting for upstream telemetry');
});
test('SDK meter colors measured remaining capacity and never paints unknown as empty', () => {
  const now = Date.parse('2026-10-26T12:00:00Z');
  const fresh = { unit: 'percent', observation_state: 'observed', observed_at: '2026-10-26T11:59:00Z' };
  assert.deepEqual(sdkMeter({ ...fresh, remaining_percent: 85 }, now), { known: true, pct: 85, tone: 'ok', dim: false });
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 70 }, now).tone, 'warn');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 30 }, now).tone, 'warn');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 29.9 }, now).tone, 'err');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 0 }, now).known, true);
  assert.deepEqual(sdkMeter({ ...fresh }, now), { known: false, pct: 0, tone: 'muted', dim: true });
  assert.equal(sdkMeter({ ...fresh, limit_reached: true }, now).tone, 'err');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 60, observation_state: 'stale' }, now).dim, true);
});

test('splitPoolBuckets sorts ascending by remaining percentage (most critical first) with stable order on ties', () => {
  const b1 = { name: 'b1', remaining_percentage: 80 };
  const b2 = { name: 'b2', unit: 'percent', remaining_percent: 15.5 };
  const b3 = { name: 'b3', remaining_percentage: 45 };
  const b4 = { name: 'b4_tie1', remaining_percentage: 45 };
  const b5 = { name: 'b5_tie2', unit: 'percent', remaining_percent: 45 };
  const { shown } = splitPoolBuckets([b1, b2, b3, b4, b5], 10);
  assert.deepEqual(shown.map((b) => b.name), ['b2', 'b3', 'b4_tie1', 'b5_tie2', 'b1']);
});

test('splitPoolBuckets sorts unknown buckets with limit_reached as 0', () => {
  const sdkUnknownExhausted = { name: 'sdk-exhausted', unit: 'percent', remaining_percent: null, limit_reached: true };
  const creditExhausted = { name: 'credit-exhausted', is_credit_balance: true, limit_reached: true };
  const knownZero = { name: 'known-zero', remaining_percentage: 0 };
  const knownLow = { name: 'known-low', remaining_percentage: 5 };
  const knownHigh = { name: 'known-high', unit: 'percent', remaining_percent: 90 };

  const { shown } = splitPoolBuckets([knownLow, sdkUnknownExhausted, knownHigh, creditExhausted, knownZero], 10);
  assert.deepEqual(shown.map((b) => b.name), ['sdk-exhausted', 'credit-exhausted', 'known-zero', 'known-low', 'known-high']);
});

test('splitPoolBuckets sorts other unknown buckets after all known values', () => {
  const unknownSDK = { name: 'sdk-unknown', unit: 'percent', remaining_percent: null };
  const unknownCredit = { name: 'credit', is_credit_balance: true };
  const sdkNaN = { name: 'sdk-nan', unit: 'percent', remaining_percent: NaN };
  const knownHigh = { name: 'known-high', remaining_percentage: 99 };
  const knownMid = { name: 'known-mid', unit: 'percent', remaining_percent: 50 };

  const { shown } = splitPoolBuckets([unknownSDK, knownHigh, unknownCredit, knownMid, sdkNaN], 10);
  assert.deepEqual(shown.map((b) => b.name), ['known-mid', 'known-high', 'sdk-unknown', 'credit', 'sdk-nan']);
});

test('splitPoolBuckets respects the limit+1 rule to avoid hiding a single bucket', () => {
  const makeBuckets = (n) => Array.from({ length: n }, (_, i) => ({ name: `b${i}`, remaining_percentage: i * 10 }));

  // Default limit = 3: 4 buckets (<= 3 + 1) -> all shown, hidden empty
  const res4 = splitPoolBuckets(makeBuckets(4));
  assert.equal(res4.shown.length, 4);
  assert.deepEqual(res4.hidden, []);
  assert.equal(res4.minHiddenLeft, null);

  // 3 buckets (<= 3 + 1) -> all shown, hidden empty
  const res3 = splitPoolBuckets(makeBuckets(3));
  assert.equal(res3.shown.length, 3);
  assert.deepEqual(res3.hidden, []);
  assert.equal(res3.minHiddenLeft, null);

  // 5 buckets (> 3 + 1) -> shown: 3, hidden: 2
  const res5 = splitPoolBuckets(makeBuckets(5));
  assert.equal(res5.shown.length, 3);
  assert.equal(res5.hidden.length, 2);
  assert.deepEqual(res5.shown.map((b) => b.name), ['b0', 'b1', 'b2']);
  assert.deepEqual(res5.hidden.map((b) => b.name), ['b3', 'b4']);

  // Custom limit = 2: 3 buckets (<= 2 + 1) -> all shown; 4 buckets -> 2 shown, 2 hidden
  const custom3 = splitPoolBuckets(makeBuckets(3), 2);
  assert.equal(custom3.shown.length, 3);
  assert.deepEqual(custom3.hidden, []);

  const custom4 = splitPoolBuckets(makeBuckets(4), 2);
  assert.equal(custom4.shown.length, 2);
  assert.equal(custom4.hidden.length, 2);
});

test('splitPoolBuckets calculates minHiddenLeft correctly or returns null when none known', () => {
  const buckets = [
    { name: 'b0', remaining_percentage: 10 },
    { name: 'b1', remaining_percentage: 20 },
    { name: 'b2', remaining_percentage: 30 },
    { name: 'b3', unit: 'percent', remaining_percent: 45.6 },
    { name: 'b4', remaining_percentage: 75.2 },
  ];
  const res = splitPoolBuckets(buckets, 3);
  assert.equal(res.minHiddenLeft, 46);

  const bucketsWithUnknownHidden = [
    { name: 'b0', remaining_percentage: 10 },
    { name: 'b1', remaining_percentage: 20 },
    { name: 'b2', remaining_percentage: 30 },
    { name: 'b3', is_credit_balance: true },
    { name: 'b4', unit: 'percent', remaining_percent: null },
  ];
  const resUnknown = splitPoolBuckets(bucketsWithUnknownHidden, 3);
  assert.equal(resUnknown.minHiddenLeft, null);

  const bucketsMixed = [
    { name: 'b0', remaining_percentage: 10 },
    { name: 'b1', remaining_percentage: 20 },
    { name: 'b2', remaining_percentage: 30 },
    { name: 'b3', remaining_percentage: 60.4 },
    { name: 'b4', is_credit_balance: true },
  ];
  const resMixed = splitPoolBuckets(bucketsMixed, 3);
  assert.equal(resMixed.minHiddenLeft, 60);

  const bucketsZero = [
    { name: 'b0', remaining_percentage: 0 },
    { name: 'b1', remaining_percentage: 0 },
    { name: 'b2', remaining_percentage: 0 },
    { name: 'b3', remaining_percentage: 0 },
    { name: 'b4', remaining_percentage: 50 },
  ];
  const resZero = splitPoolBuckets(bucketsZero, 3);
  assert.equal(resZero.minHiddenLeft, 0);
});

test('splitPoolBuckets does not mutate the input array', () => {
  const original = [
    { name: 'c', remaining_percentage: 80 },
    { name: 'a', remaining_percentage: 10 },
    { name: 'b', remaining_percentage: 50 },
    { name: 'd', remaining_percentage: 90 },
    { name: 'e', remaining_percentage: 70 },
  ];
  const copy = [...original];
  splitPoolBuckets(original);
  assert.deepEqual(original, copy);
});

test('splitPoolBuckets treats hidden unknown buckets with limit_reached as 0 in minHiddenLeft', () => {
  const buckets = [
    { name: 'sdk-0', unit: 'percent', remaining_percent: null, limit_reached: true },
    { name: 'sdk-1', unit: 'percent', remaining_percent: null, limit_reached: true },
    { name: 'sdk-2', unit: 'percent', remaining_percent: null, limit_reached: true },
    { name: 'sdk-3', unit: 'percent', remaining_percent: null, limit_reached: true },
    { name: 'known-90', unit: 'percent', remaining_percent: 90 },
  ];
  const res = splitPoolBuckets(buckets);
  assert.equal(res.hidden.some((b) => b.limit_reached), true);
  assert.equal(res.minHiddenLeft, 0);
});


