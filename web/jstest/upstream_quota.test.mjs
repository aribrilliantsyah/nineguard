import test from 'node:test';
import assert from 'node:assert/strict';
import { upstreamBarTone, upstreamAlertRows, upstreamNeedsAttention, upstreamProviderSummary, bucketFamily, groupProviderByFamily, groupProviderBySubProvider, loadGroupBy, bucketMeter, splitPeriod, FAMILY_ORDER, bentoSpan, layoutBento } from '../static/js/views/upstream_quotas.js';
import { subProviderIconURL, initials } from '../static/js/providericons.js';

test('upstreamBarTone assigns correct status color tones', () => {
  // > 70% is ok (green)
  assert.equal(upstreamBarTone(100), 'ok');
  assert.equal(upstreamBarTone(75), 'ok');
  assert.equal(upstreamBarTone(71), 'ok');

  // 30 - 70% is warn (yellow)
  assert.equal(upstreamBarTone(70), 'warn');
  assert.equal(upstreamBarTone(50), 'warn');
  assert.equal(upstreamBarTone(30), 'warn');

  // < 30% is err (red); matches the .qbar.err / .qpct.err CSS classes
  assert.equal(upstreamBarTone(29), 'err');
  assert.equal(upstreamBarTone(22), 'err');
  assert.equal(upstreamBarTone(0), 'err');
});

test('upstreamAlertRows filters disabled and unsupported providers', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'Disabled Provider',
      quota_type: 'disabled',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          status: 'error',
          buckets: [{ name: 'b1', warning: true }],
        },
      ],
    },
    {
      provider_id: 'p2',
      provider_name: 'Empty Quota Type',
      quota_type: '',
      status: 'ok',
      connections: [
        {
          connection_id: 'c2',
          status: 'error',
        },
      ],
    },
    {
      provider_id: 'p3',
      provider_name: 'Unsupported Provider',
      quota_type: '9router',
      status: 'unsupported',
      connections: [
        {
          connection_id: 'c3',
          status: 'error',
          buckets: [{ name: 'b3', warning: true }],
        },
      ],
    },
    {
      provider_id: 'p4',
      provider_name: 'Active Provider',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c4',
          account_label: 'user@test.com',
          status: 'ok',
          buckets: [
            { name: 'session', remaining_percentage: 15, warning: true },
          ],
        },
      ],
    },
  ];

  const res = upstreamAlertRows(providers);
  assert.equal(res.total, 1);
  assert.equal(res.rows.length, 1);
  assert.equal(res.rows[0].providerName, 'Active Provider');
  assert.equal(res.rows[0].bucketName, 'session');
});

test('upstreamAlertRows includes warning-only buckets and clamps percentage', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'Claude Gateway',
      quota_type: 'nineguard',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'team@org.com',
          status: 'ok',
          buckets: [
            { name: 'normal', remaining_percentage: 80, warning: false },
            { name: 'low', remaining_percentage: 18.6, remaining: 1800, warning: true, reset_at: '2026-10-25T15:00:00Z' },
            { name: 'negative_pct', remaining_percentage: -5, remaining: 0, warning: true },
            { name: 'over_100', remaining_percentage: 120, remaining: 500, warning: true },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 3);
  assert.equal(rows.length, 3);

  // negative_pct clamped to 0
  assert.equal(rows[0].bucketName, 'negative_pct');
  assert.equal(rows[0].percent, 0);
  assert.equal(rows[0].tone, 'err');

  // low bucket rounded to 19
  assert.equal(rows[1].bucketName, 'low');
  assert.equal(rows[1].percent, 19);
  assert.equal(rows[1].tone, 'err');
  assert.equal(rows[1].resetAt, '2026-10-25T15:00:00Z');

  // over_100 clamped to 100
  assert.equal(rows[2].bucketName, 'over_100');
  assert.equal(rows[2].percent, 100);
  assert.equal(rows[2].tone, 'ok');
});

test('upstreamAlertRows handles credit exhausted buckets', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'OpenAI Credits',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'billing@org.com',
          status: 'ok',
          buckets: [
            {
              name: 'Credits Zero',
              is_credit_balance: true,
              remaining: 0,
              remaining_percentage: 0,
              warning: true,
            },
            {
              name: 'Credits Positive Low',
              is_credit_balance: true,
              remaining: 450, // $4.50
              remaining_percentage: 10,
              warning: true,
            },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 2);
  assert.equal(rows[0].bucketName, 'Credits Zero');
  assert.equal(rows[0].tone, 'err'); // remaining <= 0 uses tone 'err'
  assert.equal(rows[0].isCredit, true);
  assert.equal(rows[0].remaining, 0);

  assert.equal(rows[1].bucketName, 'Credits Positive Low');
  assert.equal(rows[1].tone, 'err'); // 10% is err via upstreamBarTone
  assert.equal(rows[1].isCredit, true);
  assert.equal(rows[1].remaining, 450);
});

test('upstreamAlertRows captures error and degraded problem rows', () => {
  const providers = [
    {
      provider_id: 'p_err_noconn',
      provider_name: 'Failed Provider',
      quota_type: '9router',
      status: 'error',
      error_msg: 'upstream auth failure',
      connections: [],
    },
    {
      provider_id: 'p_deg_noconn',
      provider_name: 'Degraded Provider',
      quota_type: '9router',
      status: 'degraded',
      error_msg: 'cache expired',
      connections: [],
    },
    {
      provider_id: 'p_conn_issues',
      provider_name: 'Multi Conn',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c_err',
          account_label: 'err@test.com',
          status: 'error',
          error_msg: 'rate limited 429',
          buckets: [],
        },
        {
          connection_id: 'c_deg',
          account_label: 'deg@test.com',
          status: 'degraded',
          error_msg: 'slow response',
          buckets: [],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers);
  assert.equal(total, 4);

  // Errors first
  const errors = rows.filter((r) => r.status === 'error');
  assert.equal(errors.length, 2);
  assert.deepEqual(errors.map((e) => e.tone), ['err', 'err']);

  // Degraded last
  const degraded = rows.filter((r) => r.status === 'degraded');
  assert.equal(degraded.length, 2);
  assert.deepEqual(degraded.map((d) => d.tone), ['warn', 'warn']);
});

test('upstreamAlertRows sort order: error problems -> buckets ascending -> degraded problems', () => {
  const providers = [
    {
      provider_id: 'p1',
      provider_name: 'P1',
      quota_type: '9router',
      status: 'ok',
      connections: [
        {
          connection_id: 'c1',
          account_label: 'acc1',
          status: 'degraded',
          error_msg: 'stale cache',
          buckets: [
            { name: 'bucket_20', remaining_percentage: 20, warning: true },
          ],
        },
        {
          connection_id: 'c2',
          account_label: 'acc2',
          status: 'error',
          error_msg: 'dial tcp timeout',
          buckets: [
            { name: 'bucket_5', remaining_percentage: 5, warning: true },
            { name: 'credit_exhausted', is_credit_balance: true, remaining: 0, remaining_percentage: 15, warning: true },
          ],
        },
      ],
    },
  ];

  const { rows, total } = upstreamAlertRows(providers, 10);
  assert.equal(total, 5);

  // 1. Error problem row
  assert.equal(rows[0].kind, 'problem');
  assert.equal(rows[0].status, 'error');
  assert.equal(rows[0].accountLabel, 'acc2');

  // 2. Buckets by ascending percent (credit exhausted treated as 0)
  assert.equal(rows[1].kind, 'bucket');
  assert.equal(rows[1].bucketName, 'credit_exhausted'); // treated as 0%

  assert.equal(rows[2].kind, 'bucket');
  assert.equal(rows[2].bucketName, 'bucket_5'); // 5%

  assert.equal(rows[3].kind, 'bucket');
  assert.equal(rows[3].bucketName, 'bucket_20'); // 20%

  // 3. Degraded problem row
  assert.equal(rows[4].kind, 'problem');
  assert.equal(rows[4].status, 'degraded');
  assert.equal(rows[4].accountLabel, 'acc1');
});

test('upstreamAlertRows truncates to max and reports total', () => {
  const buckets = Array.from({ length: 10 }, (_, i) => ({
    name: `bucket_${i}`,
    remaining_percentage: i + 1,
    warning: true,
  }));

  const providers = [
    {
      provider_id: 'p1',
      quota_type: '9router',
      status: 'ok',
      connections: [
        { connection_id: 'c1', buckets },
      ],
    },
  ];

  const defaultRes = upstreamAlertRows(providers);
  assert.equal(defaultRes.total, 10);
  assert.equal(defaultRes.rows.length, 6); // default max = 6

  const customRes = upstreamAlertRows(providers, 3);
  assert.equal(customRes.total, 10);
  assert.equal(customRes.rows.length, 3);
});

test('upstreamAlertRows is tolerant of null, undefined, and empty inputs', () => {
  assert.deepEqual(upstreamAlertRows(null), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows(undefined), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows([]), { rows: [], total: 0 });
  assert.deepEqual(upstreamAlertRows('invalid'), { rows: [], total: 0 });

  const sparse = [
    null,
    undefined,
    {},
    {
      provider_id: 'p_sparse',
      quota_type: '9router',
      connections: [
        null,
        {},
        {
          connection_id: 'c1',
          buckets: [null, undefined, {}, { warning: true }],
        },
      ],
    },
  ];

  const res = upstreamAlertRows(sparse);
  assert.equal(res.total, 1);
  assert.equal(res.rows[0].providerName, 'p_sparse');
  assert.equal(res.rows[0].bucketName, '');
  assert.equal(res.rows[0].percent, 0);
  assert.equal(res.rows[0].tone, 'err');
});

test('upstreamNeedsAttention flags errors, degradation and warning buckets', () => {
  assert.equal(upstreamNeedsAttention(null), false);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'ok', buckets: [{ warning: false }] }] }), false);
  assert.equal(upstreamNeedsAttention({ status: 'degraded' }), true);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'error' }] }), true);
  assert.equal(upstreamNeedsAttention({ status: 'ok', connections: [{ status: 'ok', buckets: [{ warning: true }] }] }), true);
});

test('upstreamProviderSummary reports connections, lowest remaining and next reset', () => {
  const now = Date.parse('2026-10-25T12:00:00Z');
  const p = { connections: [
    { buckets: [{ total: 100, remaining_percentage: 64, reset_at: '2026-10-25T15:00:00Z' }] },
    { buckets: [{ total: 100, remaining_percentage: 12.4, reset_at: '2026-10-25T13:00:00Z' }, { is_credit_balance: true, total: 0, remaining_percentage: 0 }] },
  ] };
  const s = upstreamProviderSummary(p, now);
  assert.match(s, /^2 connections · lowest 12% left · next reset in /);
  assert.equal(upstreamProviderSummary({ connections: [] }, now), '0 connections');
});

test('bucketFamily classifies buckets by name, scope and connection provider', () => {
  const ag = { provider: 'antigravity' };
  assert.equal(bucketFamily({ name: 'Claude & GPT (5h)' }, ag), 'claude');
  assert.equal(bucketFamily({ name: 'Claude & GPT (Weekly)' }, ag), 'claude');
  assert.equal(bucketFamily({ name: 'Gemini (Weekly)' }, ag), 'gemini');
  assert.equal(bucketFamily({ name: 'Gemini (5h)' }, ag), 'gemini');
  assert.equal(bucketFamily({ name: 'Gemini 3 Pro Image' }, ag), 'gemini');
  assert.equal(bucketFamily({ name: 'Imagen 4' }, ag), 'gemini');
  assert.equal(bucketFamily({ name: 'gpt-image-1' }, ag), 'gpt');
  assert.equal(bucketFamily({ name: '5h Session' }, { provider: 'codex' }), 'gpt');
  assert.equal(bucketFamily({ name: '5h Session' }, { provider: 'claude' }), 'claude');
  assert.equal(bucketFamily({ name: '5h Session' }, { provider: 'gemini-cli' }), 'gemini');
  assert.equal(bucketFamily({ name: '5h Session' }, ag), 'other');
  assert.equal(bucketFamily({ name: 'Premium Interactions' }, { provider: 'github' }), 'other');
  assert.equal(bucketFamily({ name: 'Credit Balance', is_credit_balance: true }, ag), 'credit');
  assert.equal(bucketFamily({ name: 'Key Allowance (daily)' }, { provider: 'nineguard' }), 'allowance');
  assert.equal(bucketFamily({ name: 'gpt-5-codex (weekly)' }, { provider: 'nineguard' }), 'gpt');
  assert.equal(bucketFamily({ name: 'claude-sonnet-5-5 (daily)' }, { provider: 'nineguard' }), 'claude');
  // SDK per-model bucket: the model id lives in scope, the name is a generic window.
  assert.equal(bucketFamily({ name: '5-hour window', scope: 'gpt-5-codex' }, ag), 'gpt');
  assert.equal(bucketFamily({ name: '5-hour window', scope: 'account' }, ag), 'other');
  assert.equal(bucketFamily({ name: 'Weekly o3 usage' }, ag), 'gpt');
  assert.equal(bucketFamily(null, null), 'other');
});

test('groupProviderByFamily builds family cards, attention list and keeps account grouping', () => {
  const bk = (name, extra = {}) => ({ name, remaining_percentage: 50, total: 100, ...extra });
  const provider = { connections: [
    { connection_id: 'a', provider: 'antigravity', status: 'ok', buckets: [bk('Gemini (5h)'), bk('Claude & GPT (5h)'), bk('Gemini 3 Pro Image'), bk('Credit Balance', { is_credit_balance: true })] },
    { connection_id: 'b', provider: 'antigravity', status: 'ok', buckets: [bk('Gemini (5h)')] },
    { connection_id: 'c', provider: 'codex', status: 'error', error_msg: '401', buckets: [] },
    { connection_id: 'd', provider: 'codex', status: 'ok', buckets: [] },
    { connection_id: 'e', provider: 'codex', status: 'ok', buckets: [bk('5h Session')] },
  ] };
  const { cards, attention } = groupProviderByFamily(provider);
  assert.deepEqual(cards.map((c) => c.family), ['claude', 'gemini', 'gpt', 'credit']);
  assert.deepEqual(cards.map((c) => c.family), FAMILY_ORDER.filter((f) => cards.some((c) => c.family === f)));
  const gemini = cards.find((c) => c.family === 'gemini');
  assert.deepEqual(gemini.groups.map((g) => g.conn.connection_id), ['a', 'b']);
  assert.equal(gemini.groups[0].buckets.length, 2);
  assert.deepEqual(attention.map((c) => c.connection_id), ['c', 'd']);

  assert.deepEqual(groupProviderByFamily({ connections: [] }), { cards: [], attention: [] });
  assert.deepEqual(groupProviderByFamily(null), { cards: [], attention: [] });
});

test('groupProviderByFamily keeps degraded connections with an error message in attention', () => {
  const b = { name: 'Gemini (5h)', remaining_percentage: 40, total: 100 };
  const { cards, attention } = groupProviderByFamily({ connections: [
    { connection_id: 'x', status: 'degraded', error_msg: 'slow', buckets: [b] },
    { connection_id: 'y', status: 'degraded', buckets: [b] },
  ] });
  assert.deepEqual(attention.map((c) => c.connection_id), ['x']);
  assert.equal(cards[0].groups.length, 2);
});

test('bucketMeter reports limit reached, unknown and stale states', () => {
  assert.deepEqual(bucketMeter({ remaining_percentage: 0, total: 100 }), { pct: 0, tone: 'err', dim: false, limit: true, label: 'Limit reached' });
  assert.equal(bucketMeter({ remaining_percentage: 18.64, total: 100 }).label, '18.6% left');
  assert.equal(bucketMeter({ remaining_percentage: 150, total: 100 }).pct, 100);
  const sdk = { unit: 'percent', remaining_percent: 0, observation_state: 'observed' };
  assert.equal(bucketMeter(sdk).label, 'Limit reached');
  assert.equal(bucketMeter({ unit: 'percent', remaining_percent: null, limit_reached: true }).label, 'Limit reached');
  const unknown = bucketMeter({ unit: 'percent', remaining_percent: null });
  assert.equal(unknown.label, 'Unknown');
  assert.equal(unknown.tone, 'muted');
  assert.equal(unknown.dim, true);
  const stale = bucketMeter({ unit: 'percent', remaining_percent: 55, observation_state: 'stale', observed_at: new Date().toISOString() });
  assert.equal(stale.dim, true);
  assert.equal(stale.label, '55% left');
});

test('splitPeriod peels the window suffix off cascaded bucket names', () => {
  assert.deepEqual(splitPeriod('claude-sonnet-5-5 (daily)'), { name: 'claude-sonnet-5-5', period: 'daily' });
  assert.deepEqual(splitPeriod('Gemini (5h)'), { name: 'Gemini (5h)', period: null });
});

test('subProviderIconURL maps known providers to bundled files and unknown to empty', () => {
  assert.equal(subProviderIconURL('antigravity'), '/provider-icons/9router/antigravity.png');
  assert.equal(subProviderIconURL('Gemini-CLI'), '/provider-icons/9router/gemini-cli.png');
  assert.equal(subProviderIconURL('nineguard'), '/appicon.png');
  assert.equal(subProviderIconURL('some-new-provider'), '');
  assert.equal(subProviderIconURL(undefined), '');
  assert.equal(initials('alice@acme.dev'), 'AL');
  assert.equal(initials(''), '?');
});

test('groupProviderBySubProvider builds sub-provider cards and attention list in first-appearance order', () => {
  const bk = (name, extra = {}) => ({ name, remaining_percentage: 50, total: 100, ...extra });
  const provider = { connections: [
    { connection_id: 'ag1', provider: 'antigravity', status: 'ok', buckets: [bk('Gemini (5h)'), bk('Claude & GPT (5h)')] },
    { connection_id: 'cx1', provider: 'codex', status: 'ok', buckets: [bk('5h Session')] },
    { connection_id: 'ag2', provider: 'antigravity', status: 'ok', buckets: [bk('Gemini (Weekly)')] },
    { connection_id: 'err1', provider: 'github', status: 'error', error_msg: 'unauthorized', buckets: [] },
  ] };
  const { cards, attention } = groupProviderBySubProvider(provider);
  assert.deepEqual(cards.map((c) => c.subProvider), ['antigravity', 'codex']);
  assert.deepEqual(cards[0].groups.map((g) => g.conn.connection_id), ['ag1', 'ag2']);
  assert.deepEqual(cards[1].groups.map((g) => g.conn.connection_id), ['cx1']);
  assert.deepEqual(cards[0].groups[0].buckets.map((b) => b.name), ['Claude & GPT (5h)', 'Gemini (5h)']);
  assert.deepEqual(attention.map((c) => c.connection_id), ['err1']);

  assert.deepEqual(groupProviderBySubProvider({ connections: [] }), { cards: [], attention: [] });
  assert.deepEqual(groupProviderBySubProvider(null), { cards: [], attention: [] });
});

test('loadGroupBy defaults to account and validates stored choices', () => {
  const original = globalThis.localStorage;
  try {
    delete globalThis.localStorage;
    assert.equal(loadGroupBy(), 'account');

    let stored = null;
    globalThis.localStorage = {
      getItem: () => stored,
    };
    assert.equal(loadGroupBy(), 'account');

    stored = 'invalid';
    assert.equal(loadGroupBy(), 'account');

    stored = 'provider';
    assert.equal(loadGroupBy(), 'provider');

    stored = 'model';
    assert.equal(loadGroupBy(), 'model');

    stored = 'account';
    assert.equal(loadGroupBy(), 'account');
  } finally {
    if (original !== undefined) {
      globalThis.localStorage = original;
    } else {
      delete globalThis.localStorage;
    }
  }
});

test('bentoSpan calculates column span matching mockup algorithm', () => {
  // (6, 4, true) -> 3
  assert.equal(bentoSpan(6, 4, true), 3);
  // (5, 4, true) -> 3
  assert.equal(bentoSpan(5, 4, true), 3);
  // (4, 4, false) -> 2
  assert.equal(bentoSpan(4, 4, false), 2);
  // (4, 4, true) -> 4
  // In mockup's code: if (totalCols >= 4 && hasSmallGroups && n >= 4) { if (n === 6 || n === 5) span = 3; } else if ...
  // When n=4 and hasSmallGroups=true, outer if branch enters, inner if is false, else-if is skipped, span remains 4.
  assert.equal(bentoSpan(4, 4, true), 4);
  // (1, 4, true) -> 1
  assert.equal(bentoSpan(1, 4, true), 1);
  // (6, 2, true) -> 2
  assert.equal(bentoSpan(6, 2, true), 2);
  // (3, 4, false) -> 3
  assert.equal(bentoSpan(3, 4, false), 3);
  // (6, 4, false) -> 4
  assert.equal(bentoSpan(6, 4, false), 4);
});

test('layoutBento returns early without error when element is missing or collapsed', () => {
  assert.equal(layoutBento(null), undefined);
  assert.equal(layoutBento(undefined), undefined);
  assert.equal(layoutBento({ clientWidth: 0 }), undefined);
});

test('layoutBento distributes 1-card groups into multiple columns when totalCols > 1', () => {
  const origDoc = globalThis.document;
  try {
    function el(tag, cls = '') {
      const o = {
        tagName: tag,
        className: cls,
        children: [],
        style: {},
        appendChild(c) { o.children.push(c); },
        replaceChildren(...cs) { o.children = [...cs]; },
        querySelector() { return o._body || null; },
        getBoundingClientRect() { return { height: 100 }; }
      };
      return o;
    }
    globalThis.document = {
      createElement(tag) { return el(tag); }
    };

    const box1 = el('div', 'gb-box');
    const body1 = el('div', 'gb-body');
    body1.appendChild(el('div', 'card1'));
    box1._body = body1;

    const box2 = el('div', 'gb-box');
    const body2 = el('div', 'gb-body');
    body2.appendChild(el('div', 'card2'));
    box2._body = body2;

    const stage = el('div', 'gb-stage');
    stage.classList = { contains: () => false };
    stage.clientWidth = 700; // totalCols = 2
    stage.appendChild(box1);
    stage.appendChild(box2);

    layoutBento(stage);
    const board = stage.children[0];
    assert.equal(board.className, 'gb-board');
    assert.equal(board.children.length, 2);
    assert.equal(board.children[0].className, 'gb-board-col');
    assert.equal(board.children[0].style.flex, '1 1 0%');
    assert.equal(board.children[1].style.flex, '1 1 0%');
    assert.equal(board.children[0].children[0], box1);
    assert.equal(board.children[1].children[0], box2);
  } finally {
    globalThis.document = origDoc;
  }
});

test('layoutBento caps side columns to unitGroups length when totalCols exceeds unit groups', () => {
  const origDoc = globalThis.document;
  try {
    function el(tag, cls = '') {
      const o = {
        tagName: tag,
        className: cls,
        children: [],
        style: {},
        appendChild(c) { o.children.push(c); },
        replaceChildren(...cs) { o.children = [...cs]; },
        querySelector() { return o._body || null; },
        getBoundingClientRect() { return { height: 100 }; }
      };
      return o;
    }
    globalThis.document = {
      createElement(tag) { return el(tag); }
    };

    const box1 = el('div', 'gb-box');
    const body1 = el('div', 'gb-body');
    body1.appendChild(el('div', 'card1'));
    body1.appendChild(el('div', 'card2'));
    box1._body = body1;

    const box2 = el('div', 'gb-box');
    const body2 = el('div', 'gb-body');
    body2.appendChild(el('div', 'card3'));
    box2._body = body2;

    const stage = el('div', 'gb-stage');
    stage.classList = { contains: () => false };
    stage.clientWidth = 1580; // totalCols = 5
    stage.appendChild(box1);
    stage.appendChild(box2);

    layoutBento(stage);
    const board = stage.children[0];
    assert.equal(board.className, 'gb-board');
    assert.equal(board.children.length, 2);
    assert.equal(board.children[0].style.flex, '2 1 0%');
    assert.equal(board.children[1].style.flex, '1 1 0%');
    assert.equal(board.children[0].children[0], box1);
    assert.equal(board.children[1].children[0], box2);
  } finally {
    globalThis.document = origDoc;
  }
});



