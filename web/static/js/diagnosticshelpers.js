// Diagnostics helpers for Traffic Explorer badges and token indicators

export function formatTokensCompact(n) {
  if (n == null || isNaN(n)) return '0';
  const num = Number(n);
  if (num >= 1e6) {
    const s = (num / 1e6).toFixed(1);
    return s.endsWith('.0') ? s.slice(0, -2) + 'M' : s + 'M';
  }
  if (num >= 1e3) {
    const s = (num / 1e3).toFixed(1);
    return s.endsWith('.0') ? s.slice(0, -2) + 'k' : s + 'k';
  }
  return String(num);
}

export function formatCacheHitRate(cacheRead, prompt) {
  if (cacheRead == null || prompt == null) return '';
  const c = Number(cacheRead);
  const p = Number(prompt);
  if (isNaN(c) || isNaN(p) || p <= 0) return '';
  const pct = Math.min(100, Math.max(0, Math.round((c / p) * 100)));
  return `${pct}%`;
}

export function thinkingBadge(thinkingReq, thinkingDel, reasoningTokens, apiFormat, model) {
  if (thinkingDel) {
    const countStr = reasoningTokens > 0 ? ` ${formatTokensCompact(reasoningTokens)}` : '';
    return {
      text: `🧠${countStr}`,
      cls: 'thinking-delivered',
      title: reasoningTokens > 0
        ? `Thinking delivered: ${Number(reasoningTokens).toLocaleString()} reasoning tokens observed`
        : 'Thinking delivered: reasoning was observed in upstream response',
    };
  }
  if (thinkingReq) {
    const isUnverifiable = (apiFormat === 'chat' || (typeof apiFormat === 'string' && apiFormat.toLowerCase() === 'chat')) &&
      typeof model === 'string' &&
      model.toLowerCase().includes('claude');
    if (isUnverifiable) {
      return {
        text: '🧠?',
        cls: 'thinking-unknown',
        title: 'Thinking was requested. Claude over the OpenAI chat format returns no observable reasoning text, so delivery cannot be verified.',
      };
    }
    return {
      text: '🧠✗',
      cls: 'thinking-lost',
      title: 'Thinking was requested but the upstream response shows no reasoning.',
    };
  }
  return null;
}

export function fallbackBadge(modelFallback, requestedModel, servedModel) {
  if (!modelFallback) return null;
  return {
    text: '⚠ fallback',
    cls: 'model-fallback',
    title: `Model fallback detected: requested ${requestedModel || '-'} → served ${servedModel || '-'}`,
  };
}
