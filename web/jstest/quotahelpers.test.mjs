import test from 'node:test';
import assert from 'node:assert/strict';
import {
  quotaPercent,
  formatQuotaUsage,
  isQuotaExhausted,
  quotaPeriodLabel,
  formatTokensCompact,
} from '../static/js/quotahelpers.js';

test('quotaPercent calculates capped percentage', () => {
  assert.equal(quotaPercent(2500, 10000), 25);
  assert.equal(quotaPercent(15000, 10000), 100);
  assert.equal(quotaPercent(0, 0), 0);
  assert.equal(quotaPercent(0, 5000), 0);
  assert.equal(quotaPercent(500, 0), 0);
});

test('formatTokensCompact formats tokens with k and M suffix', () => {
  assert.equal(formatTokensCompact(500), '500');
  assert.equal(formatTokensCompact(1000), '1k');
  assert.equal(formatTokensCompact(25000), '25k');
  assert.equal(formatTokensCompact(1500000), '1.5M');
  assert.equal(formatTokensCompact(0), '0');
});

test('formatQuotaUsage formats usage string', () => {
  assert.equal(formatQuotaUsage(25000, 50000, 'daily'), '25k / 50k daily (50%)');
  assert.equal(formatQuotaUsage(0, 0, 'none'), 'Unlimited');
  assert.equal(formatQuotaUsage(1000, 0, 'none'), 'Unlimited');
  assert.equal(formatQuotaUsage(60000, 50000, 'daily'), '60k / 50k daily (100%)');
});

test('isQuotaExhausted checks if limit met or exceeded', () => {
  assert.equal(isQuotaExhausted(50000, 50000, 'daily'), true);
  assert.equal(isQuotaExhausted(55000, 50000, 'daily'), true);
  assert.equal(isQuotaExhausted(45000, 50000, 'daily'), false);
  assert.equal(isQuotaExhausted(100000, 0, 'none'), false);
});

test('quotaPeriodLabel translates period to human label', () => {
  assert.equal(quotaPeriodLabel('daily'), 'Daily (UTC)');
  assert.equal(quotaPeriodLabel('weekly'), 'Weekly (UTC Mon)');
  assert.equal(quotaPeriodLabel('monthly'), 'Monthly (UTC 1st)');
  assert.equal(quotaPeriodLabel('total'), 'Lifetime');
  assert.equal(quotaPeriodLabel('none'), 'None');
  assert.equal(quotaPeriodLabel(''), 'None');
});
