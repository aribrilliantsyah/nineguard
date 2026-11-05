import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatCacheHitRate,
  thinkingBadge,
  fallbackBadge,
  protocolBadge,
  endpointPathForFormat,
} from '../static/js/diagnosticshelpers.js';

test('formatCacheHitRate formats percentage correctly', () => {
  assert.equal(formatCacheHitRate(900, 1000), '90%');
  assert.equal(formatCacheHitRate(0, 1000), '0%');
  assert.equal(formatCacheHitRate(0, 0), '');
  assert.equal(formatCacheHitRate(null, 1000), '');
  assert.equal(formatCacheHitRate(500, null), '');
});

test('thinkingBadge returns badge descriptor or null', () => {
  // Delivered with reasoning tokens count
  const b1 = thinkingBadge(true, true, 2100);
  assert.equal(b1?.text, '2.1k');
  assert.equal(b1?.icon, 'brain');
  assert.equal(b1?.cls, 'thinking-delivered');

  // Delivered without reasoning token count (or 0)
  const b2 = thinkingBadge(true, true, 0);
  assert.equal(b2?.text, 'Thinking');
  assert.equal(b2?.icon, 'brain');
  assert.equal(b2?.cls, 'thinking-delivered');

  // Requested but not delivered (thinking lost)
  const b3 = thinkingBadge(true, false, 0);
  assert.equal(b3?.text, 'Lost');
  assert.equal(b3?.icon, 'brain');
  assert.equal(b3?.cls, 'thinking-lost');

  // Not requested
  const b4 = thinkingBadge(false, false, 0);
  assert.equal(b4, null);

  // Unverified: upstream did not report whether it reasoned (thinkingUnverified = true)
  const b5 = thinkingBadge(true, false, 0, 'chat', 'deepseek-chat', true);
  assert.equal(b5?.text, 'Unverified');
  assert.equal(b5?.icon, 'brain');
  assert.equal(b5?.cls, 'thinking-unverified');
  assert.equal(
    b5?.title,
    'Thinking was requested but the upstream did not report whether it reasoned'
  );

  // Non-Claude chat model requested but not delivered (thinkingUnverified = false) returns lost
  const b7 = thinkingBadge(true, false, 0, 'chat', 'gpt-4o', false);
  assert.equal(b7?.text, 'Lost');
  assert.equal(b7?.cls, 'thinking-lost');

  // Delivered Claude chat request still returns delivered
  const b9 = thinkingBadge(true, true, 2100, 'chat', 'claude-3-7-sonnet', false);
  assert.equal(b9?.text, '2.1k');
  assert.equal(b9?.cls, 'thinking-delivered');
});

test('fallbackBadge returns badge descriptor or null', () => {
  const f1 = fallbackBadge(true, '9router/claude-opus-4.5', 'claude-sonnet-4-5-20250929');
  assert.equal(f1?.text, 'Fallback');
  assert.equal(f1?.icon, 'alert');
  assert.equal(f1?.cls, 'model-fallback');
  assert.match(f1?.title, /requested 9router\/claude-opus-4.5 → served claude-sonnet-4-5-20250929/);

  const f2 = fallbackBadge(false, '9router/claude-opus-4.5', 'claude-opus-4-5-20251101');
  assert.equal(f2, null);
});

test('protocolBadge returns badge descriptor or null', () => {
  // messages
  const b1 = protocolBadge('messages');
  assert.deepEqual(b1, {
    text: 'messages',
    cls: 'proto-messages',
    title: 'Anthropic Messages API (/v1/messages)',
  });

  // responses
  const b2 = protocolBadge('responses');
  assert.deepEqual(b2, {
    text: 'responses',
    cls: 'proto-responses',
    title: 'OpenAI Responses API (/v1/responses)',
  });

  // chat
  const b3 = protocolBadge('chat');
  assert.deepEqual(b3, {
    text: 'chat',
    cls: 'proto-chat',
    title: 'OpenAI Chat Completions API (/v1/chat/completions)',
  });

  // case and whitespace normalization
  assert.equal(protocolBadge('  MESSAGES  ')?.cls, 'proto-messages');
  assert.equal(protocolBadge(' Chat ')?.cls, 'proto-chat');
  assert.equal(protocolBadge('Responses')?.cls, 'proto-responses');

  // fallback to null for other formats / empty / null
  assert.equal(protocolBadge(''), null);
  assert.equal(protocolBadge('   '), null);
  assert.equal(protocolBadge('other'), null);
  assert.equal(protocolBadge('completions'), null);
  assert.equal(protocolBadge(null), null);
  assert.equal(protocolBadge(undefined), null);
});

test('endpointPathForFormat maps formats to endpoints with fallback', () => {
  assert.equal(endpointPathForFormat('messages'), '/v1/messages');
  assert.equal(endpointPathForFormat(' Messages '), '/v1/messages');
  assert.equal(endpointPathForFormat('responses'), '/v1/responses');
  assert.equal(endpointPathForFormat(' Responses '), '/v1/responses');
  assert.equal(endpointPathForFormat('chat'), '/v1/chat/completions');
  assert.equal(endpointPathForFormat(''), '/v1/chat/completions');
  assert.equal(endpointPathForFormat('other'), '/v1/chat/completions');
  assert.equal(endpointPathForFormat(null), '/v1/chat/completions');
  assert.equal(endpointPathForFormat(undefined), '/v1/chat/completions');
});
