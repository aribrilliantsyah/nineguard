import test from 'node:test';
import assert from 'node:assert/strict';

import { formatCost, costTooltip, formatPriceRate } from '../static/js/costhelpers.js';

test('formatCost with USD standard amounts', () => {
  assert.equal(formatCost(12.34, 'USD', 1.0, 0), '$12.34');
  assert.equal(formatCost(1234.56, 'USD', 1.0, 0), '$1,234.56');
  assert.equal(formatCost(0, 'USD', 1.0, 0), '$0.00');
});

test('formatCost with IDR zero decimals and sub-unit', () => {
  const idr = formatCost(100, 'IDR', 18000, 0);
  assert.ok(idr.includes('1.800.000') || idr.includes('1,800,000'), `got ${idr}`);

  // Sub-unit: positive USD amount < 1 IDR
  const sub = formatCost(0.00001, 'IDR', 18000, 0);
  assert.equal(sub, '<Rp 1');
});

test('formatCost with unpriced requests', () => {
  assert.equal(formatCost(10.5, 'USD', 1.0, 3), '≥ $10.50');
  assert.equal(formatCost(0, 'USD', 1.0, 1), '≥ $0.00');
});

test('formatCost with null or unpriced row', () => {
  assert.equal(formatCost(null, 'USD', 1.0, 0), '—');
  assert.equal(formatCost(undefined, 'USD', 1.0, 0), '—');
});

test('formatPriceRate formats per 1M rates', () => {
  assert.equal(formatPriceRate(3.0, 15.0), '$3.00 / $15.00 per 1M');
  assert.equal(formatPriceRate(null, null), 'No price');
});

test('costTooltip includes ECB notice when converted', () => {
  const tipUSD = costTooltip(10, 'USD', 1.0);
  assert.ok(tipUSD.includes('API list-price equivalent, not actual spend'));
  assert.ok(!tipUSD.includes('converted at today\'s rate'));

  const tipIDR = costTooltip(10, 'IDR', 18031.5, '2026-10-16');
  assert.ok(tipIDR.includes('converted at today\'s rate'));
  assert.ok(tipIDR.includes('18,031.5') || tipIDR.includes('18031.5'));
});
