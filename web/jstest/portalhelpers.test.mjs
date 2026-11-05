import test from 'node:test';
import assert from 'node:assert/strict';

import {
  isValidKeyFormat,
  maskKey,
  buildHeaders,
  getStoredPortalKey,
  setStoredPortalKey,
  clearStoredPortalKey,
  budgetSummary,
  activitySentence,
  adaptDailyTokens
} from '../static/js/portalhelpers.js';

test('isValidKeyFormat validates key structure', () => {
  assert.equal(isValidKeyFormat(''), false);
  assert.equal(isValidKeyFormat('   '), false);
  assert.equal(isValidKeyFormat(null), false);
  assert.equal(isValidKeyFormat(undefined), false);
  assert.equal(isValidKeyFormat('invalid-key'), false);
  assert.equal(isValidKeyFormat('sk-ng-abc1234567890'), true);
  assert.equal(isValidKeyFormat('sk-proj-xyz123'), true);
  assert.equal(isValidKeyFormat('Bearer sk-ng-abc1234'), true);
});

test('maskKey masks token safely', () => {
  assert.equal(maskKey(''), '-');
  assert.equal(maskKey(null), '-');
  assert.equal(maskKey('sk-1234'), 'sk-...34');
  assert.equal(maskKey('sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
  assert.equal(maskKey('Bearer sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
});

test('buildHeaders constructs authorization header', () => {
  assert.deepEqual(buildHeaders('sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });

  // Strips leading 'Bearer ' if user input already includes it
  assert.deepEqual(buildHeaders('Bearer sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });
});

test('localStorage key management', () => {
  const mockStorage = new Map();
  const fakeLocalStorage = {
    getItem: (k) => mockStorage.get(k) || null,
    setItem: (k, v) => mockStorage.set(k, String(v)),
    removeItem: (k) => mockStorage.delete(k)
  };

  assert.equal(getStoredPortalKey(fakeLocalStorage), '');

  setStoredPortalKey('  sk-ng-test123  ', fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), 'sk-ng-test123');

  clearStoredPortalKey(fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), '');
});

test('budgetSummary: per-model budgets win over the key budget, total first, tightest row first', () => {
  const s = budgetSummary({
    key_quota: { limit: 1000, used: 10, period: 'daily' },
    model_quotas: [
      { model_id: 'a', limit: 100, used: 20, period: 'daily' },
      { model_id: 'b', limit: 100, used: 150, period: 'weekly' },
      { model_id: 'c', limit: 0, used: 5 },
      { model_id: 'd', limit: 1000, used: 999, period: 'daily', inactive: true },
    ],
  });
  assert.equal(s.kind, 'models');
  assert.deepEqual(s.rows.map((r) => r.modelId), ['b', 'a']);
  assert.equal(s.tightest.modelId, 'b');
  assert.equal(s.tightest.pct, 100);
  assert.equal(s.tightest.left, 0);
  assert.equal(s.tightest.tone, 'err');
  assert.equal(s.exhausted, 1);
  // Total over active budgets: used is capped per row (b counts 100, not 150); tone is the worst row's.
  assert.deepEqual([s.total.used, s.total.limit, s.total.left, s.total.pct, s.total.tone], [120, 200, 80, 60, 'err']);
  assert.deepEqual(s.inactive.map((r) => r.modelId), ['d']);
});

test('budgetSummary: key budget tone crosses warn at 80%', () => {
  assert.equal(budgetSummary({ key_quota: { limit: 1000, used: 799, period: 'daily' } }).tone, 'ok');
  const near = budgetSummary({ key_quota: { limit: 1000, used: 800, period: 'daily' } });
  assert.equal(near.kind, 'key');
  assert.equal(near.tone, 'warn');
  assert.equal(near.left, 200);
});

test('budgetSummary: no limits is unlimited, missing data is unknown', () => {
  assert.equal(budgetSummary({ upstream_pools: [] }).kind, 'unlimited');
  assert.equal(budgetSummary({ key_quota: { limit: 0, used: 5 } }).kind, 'unlimited');
  assert.equal(budgetSummary(null).kind, 'unknown');
});

test('activitySentence formats summary correctly for ranges and models', () => {
  assert.equal(activitySentence(null), '');
  assert.equal(activitySentence({ requests: 0 }), '');

  // 2+ models, 30d default
  const s1 = {
    requests: 1203,
    tokens: { total: 12400000 },
    models: [
      { model_id: 'claude-sonnet-5-5', total: 7700000, pct_tokens: 62.1 },
      { model_id: 'gemini-3.8-flash', total: 2600000, pct_tokens: 21.0 },
      { model_id: 'glm-5.3', total: 1300000, pct_tokens: 10.5 }
    ]
  };
  assert.equal(
    activitySentence(s1, '30d'),
    'Last 30 days: 12.4M tokens over 1,203 requests. Mostly claude-sonnet-5-5 (62%), then gemini-3.8-flash (21%).'
  );

  // 1 model, 7d
  const s2 = {
    requests: 5,
    tokens: { total: 500 },
    models: [
      { model_id: 'gpt-4o', total: 500, pct_tokens: 100 }
    ]
  };
  assert.equal(
    activitySentence(s2, '7d'),
    'Last 7 days: 500 tokens over 5 requests. Mostly gpt-4o (100%).'
  );

  // 0 models, all time
  const s3 = {
    requests: 1,
    tokens: { total: 100 },
    models: []
  };
  assert.equal(
    activitySentence(s3, 'all'),
    'All time: 100 tokens over 1 requests.'
  );
});

test('adaptDailyTokens transforms usage data into volumeChart structure', () => {
  assert.equal(adaptDailyTokens(null), null);
  assert.equal(adaptDailyTokens({ daily: [] }), null);

  const usage = {
    daily: [
      { date: '2025-10-01', tokens: 100 },
      { date: '2025-10-02', tokens: 200 }
    ],
    models: [
      { model_id: 'm1', total: 150, daily: [{ date: '2025-10-01', total: 50 }, { date: '2025-10-02', total: 100 }] },
      { model_id: 'm2', total: 100, daily: [{ date: '2025-10-01', total: 30 }, { date: '2025-10-02', total: 70 }] },
      { model_id: 'm3', total: 30, daily: [{ date: '2025-10-01', total: 10 }, { date: '2025-10-02', total: 20 }] },
      { model_id: 'm4', total: 15, daily: [{ date: '2025-10-01', total: 5 }, { date: '2025-10-02', total: 10 }] },
      { model_id: 'm5', total: 5, daily: [{ date: '2025-10-01', total: 5 }, { date: '2025-10-02', total: 0 }] }
    ]
  };

  const res = adaptDailyTokens(usage);
  assert.ok(res);
  assert.equal(res.days.length, 2);
  assert.equal(res.chartData.bucket_nanos, 86400 * 1e9);
  assert.equal(res.chartData.buckets.length, 2);

  // 5 models -> 4 top models + Other
  assert.equal(res.series.length, 5);
  // Bottom-to-top order in series: Other at index 0, m1 at top index 4
  assert.equal(res.series[0].label, 'Other');
  assert.equal(res.series[0].color, 'var(--spark)');
  assert.deepEqual(res.series[0].levels, ['m_other']);
  assert.equal(res.series[4].label, 'm1');
  assert.equal(res.series[4].color, 'var(--lv-info)');
  assert.deepEqual(res.series[4].levels, ['m_0']);

  // Check counts in day 1 (2025-10-01)
  const b1 = res.chartData.buckets[0];
  assert.equal(b1.counts['m_0'], 50);
  assert.equal(b1.counts['m_1'], 30);
  assert.equal(b1.counts['m_2'], 10);
  assert.equal(b1.counts['m_3'], 5);
  assert.equal(b1.counts['m_other'], 5); // from m5

  // Check totals
  assert.equal(res.chartData.totals['m_0'], 150);
  assert.equal(res.chartData.totals['m_other'], 5);

  // When <= 4 models, no Other series
  const usageFew = {
    daily: [{ date: '2025-10-01', tokens: 100 }],
    models: [
      { model_id: 'm1', total: 60, daily: [{ date: '2025-10-01', total: 60 }] },
      { model_id: 'm2', total: 40, daily: [{ date: '2025-10-01', total: 40 }] }
    ]
  };
  const resFew = adaptDailyTokens(usageFew);
  assert.equal(resFew.series.length, 2);
  assert.equal(resFew.series[0].label, 'm2');
  assert.equal(resFew.series[1].label, 'm1');
});
