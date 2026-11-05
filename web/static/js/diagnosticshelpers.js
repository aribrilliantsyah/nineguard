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

export function thinkingBadge(thinkingReq, thinkingDel, reasoningTokens, apiFormat, model, thinkingUnverified) {
  if (thinkingDel) {
    return {
      icon: 'brain',
      text: reasoningTokens > 0 ? formatTokensCompact(reasoningTokens) : 'Thinking',
      cls: 'thinking-delivered',
      title: reasoningTokens > 0
        ? `Thinking delivered: ${Number(reasoningTokens).toLocaleString()} reasoning tokens observed`
        : 'Thinking delivered: reasoning was observed in upstream response',
    };
  }
  if (thinkingReq) {
    if (thinkingUnverified) {
      return {
        icon: 'brain',
        text: 'Unverified',
        cls: 'thinking-unverified',
        title: 'Thinking was requested but the upstream did not report whether it reasoned',
      };
    }
    return {
      icon: 'brain',
      text: 'Lost',
      cls: 'thinking-lost',
      title: 'Thinking was requested but the upstream response shows no reasoning.',
    };
  }
  return null;
}

export function fallbackBadge(modelFallback, requestedModel, servedModel) {
  if (!modelFallback) return null;
  return {
    icon: 'alert',
    text: 'Fallback',
    cls: 'model-fallback',
    title: `Model fallback detected: requested ${requestedModel || '-'} → served ${servedModel || '-'}`,
  };
}

export function protocolBadge(apiFormat) {
  const fmt = typeof apiFormat === 'string' ? apiFormat.trim().toLowerCase() : '';
  if (fmt === 'messages') {
    return { text: 'messages', cls: 'proto-messages', title: 'Anthropic Messages API (/v1/messages)' };
  }
  if (fmt === 'responses') {
    return { text: 'responses', cls: 'proto-responses', title: 'OpenAI Responses API (/v1/responses)' };
  }
  if (fmt === 'chat') {
    return { text: 'chat', cls: 'proto-chat', title: 'OpenAI Chat Completions API (/v1/chat/completions)' };
  }
  return null;
}

export function endpointPathForFormat(apiFormat) {
  const fmt = typeof apiFormat === 'string' ? apiFormat.trim().toLowerCase() : '';
  if (fmt === 'messages') return '/v1/messages';
  if (fmt === 'responses') return '/v1/responses';
  return '/v1/chat/completions';
}

