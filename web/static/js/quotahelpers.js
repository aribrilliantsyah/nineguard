// Pure helper functions for API Key token quotas and formatting

export function quotaPercent(consumed, limit) {
  if (!limit || limit <= 0) return 0;
  if (!consumed || consumed <= 0) return 0;
  return Math.min(100, Math.round((consumed / limit) * 100));
}

export function formatTokensCompact(n) {
  if (!n || n <= 0) return '0';
  if (n >= 1_000_000) {
    const val = (n / 1_000_000).toFixed(1);
    return (val.endsWith('.0') ? val.slice(0, -2) : val) + 'M';
  }
  if (n >= 1_000) {
    const val = (n / 1_000).toFixed(1);
    return (val.endsWith('.0') ? val.slice(0, -2) : val) + 'k';
  }
  return String(n);
}

export function formatQuotaUsage(consumed, limit, period) {
  if (!period || period === 'none' || !limit || limit <= 0) {
    return 'Unlimited';
  }
  const pct = quotaPercent(consumed, limit);
  const usedStr = formatTokensCompact(consumed);
  const limitStr = formatTokensCompact(limit);
  return `${usedStr} / ${limitStr} ${period} (${pct}%)`;
}

export function isQuotaExhausted(consumed, limit, period) {
  if (!period || period === 'none' || !limit || limit <= 0) {
    return false;
  }
  return consumed >= limit;
}

export function quotaPeriodLabel(period) {
  switch (period) {
    case 'daily':
      return 'Daily (UTC)';
    case 'weekly':
      return 'Weekly (UTC Mon)';
    case 'monthly':
      return 'Monthly (UTC 1st)';
    case 'total':
      return 'Lifetime';
    default:
      return 'None';
  }
}
