// NineGuard Client Key Self-Service Portal.
// Answers, in order: can I keep working (budget), what did I use (activity), how do I connect, what can I call.
import { h, icon, toast, fmtNum, fmtCompact, fmtClock, timeZone, tzLabel, copy } from './ui.js';
import {
  getStoredPortalKey,
  setStoredPortalKey,
  clearStoredPortalKey,
  isValidKeyFormat,
  maskKey,
  buildHeaders,
  budgetSummary,
  periodWord,
  activitySentence,
  adaptDailyTokens
} from './portalhelpers.js';
import { volumeChart } from './chart.js';
import { formatCost } from './costhelpers.js';
import { formatTokensCompact } from './quotahelpers.js';
import { isPercentBucket, remainingLabel, observationLabel, telemetryStateLabel, sdkMeter, remainingTone, splitPoolBuckets } from './upstreamtelemetry.js';
import { iconTile, subProviderIconURL } from './providericons.js';
import { formatResetIn, shortModel } from './modelquotas.js';

const root = document.getElementById('portal-app');

const RANGES = [['7d', '7 days'], ['30d', '30 days'], ['all', 'All time']];
const MODEL_LIST_PREVIEW = 12;

let activeKey = getStoredPortalKey();
let activeRange = '30d';
let quotaData = null;
let usageData = null;
let modelsData = null;
let loading = false;
let errorMsg = '';
let modelFilter = '';
let showAllModels = false;
const expandedPools = new Set();

async function fetchJSON(url, key) {
  const resp = await fetch(url, { headers: { ...buildHeaders(key), 'Accept': 'application/json' } });
  if (resp.status === 401) throw new Error('UNAUTHORIZED');

  const text = await resp.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch { /* non-JSON body */ }

  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    if (data && data.error) msg = typeof data.error === 'string' ? data.error : (data.error.message || msg);
    throw new Error(msg);
  }
  return data;
}

function toggleTheme() {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try { localStorage.setItem('nineguard_theme', next); } catch { /* storage unavailable */ }
}

function themeButton(cls) {
  const btn = h('button', { class: cls, type: 'button', title: 'Switch light / dark theme', 'aria-label': 'Switch light / dark theme' });
  const paint = () => btn.replaceChildren(icon(document.documentElement.dataset.theme === 'dark' ? 'sun' : 'moon'));
  btn.addEventListener('click', () => { toggleTheme(); paint(); });
  paint();
  return btn;
}

async function validateAndSaveKey(rawKey) {
  const key = rawKey.trim();
  if (!isValidKeyFormat(key)) throw new Error('Keys start with sk- (for example sk-ng-...).');
  await fetchJSON('/v1/quota', key);
  setStoredPortalKey(key);
  activeKey = key;
}

async function loadPortalData() {
  if (!activeKey) return;
  loading = true;
  errorMsg = '';
  render();

  const tz = timeZone() || 'UTC';
  const soft = (fallback) => (err) => {
    if (err.message === 'UNAUTHORIZED') throw err;
    return fallback;
  };
  try {
    const [qRes, uRes, mRes] = await Promise.all([
      fetchJSON('/v1/quota', activeKey).catch(soft(null)),
      fetchJSON(`/v1/usage?range=${activeRange}&tz=${encodeURIComponent(tz)}`, activeKey).catch(soft(null)),
      fetchJSON('/v1/models', activeKey).catch(soft({ data: [] })),
    ]);
    quotaData = qRes;
    usageData = uRes;
    modelsData = (mRes && Array.isArray(mRes.data)) ? mRes.data : [];
    if (!qRes && !uRes) errorMsg = 'NineGuard did not answer the quota or usage request.';
  } catch (err) {
    if (err.message === 'UNAUTHORIZED') {
      clearStoredPortalKey();
      activeKey = '';
      toast('This key was revoked or disabled. Enter a current key.', 'error');
    } else {
      errorMsg = err.message || 'Could not load usage data.';
    }
  } finally {
    loading = false;
    render();
  }
}

function disconnect() {
  clearStoredPortalKey();
  activeKey = '';
  quotaData = usageData = modelsData = null;
  render();
}

// ── Sign-in ──

function renderLoginView() {
  let showKey = false;
  const keyInput = h('input', {
    class: 'input pt-key-input',
    type: 'password',
    placeholder: 'sk-ng-...',
    autocomplete: 'off',
    spellcheck: 'false',
    'aria-label': 'NineGuard API key',
    autofocus: true
  });
  const eyeBtn = h('button', {
    class: 'btn', type: 'button', 'aria-label': 'Show key',
    onclick: () => {
      showKey = !showKey;
      keyInput.type = showKey ? 'text' : 'password';
      eyeBtn.setAttribute('aria-label', showKey ? 'Hide key' : 'Show key');
      eyeBtn.replaceChildren(icon(showKey ? 'eye-off' : 'eye'));
    }
  }, icon('eye'));
  const submitBtn = h('button', { class: 'btn btn-primary btn-block', type: 'submit' }, icon('key'), 'Open my usage');
  const errBox = h('p', { class: 'note err', role: 'alert', hidden: true });

  const form = h('form', {
    class: 'auth-form',
    onsubmit: async (e) => {
      e.preventDefault();
      if (!keyInput.value.trim()) return;
      submitBtn.disabled = true;
      submitBtn.replaceChildren(icon('refresh'), 'Checking key...');
      errBox.hidden = true;
      try {
        await validateAndSaveKey(keyInput.value);
        await loadPortalData();
      } catch (err) {
        errBox.textContent = err.message === 'UNAUTHORIZED'
          ? 'NineGuard does not recognise this key, or it is disabled.'
          : (err.message || 'Could not check this key.');
        errBox.hidden = false;
      } finally {
        submitBtn.disabled = false;
        submitBtn.replaceChildren(icon('key'), 'Open my usage');
      }
    }
  },
    h('div', { class: 'pt-key-row' }, keyInput, eyeBtn),
    errBox,
    submitBtn,
    h('p', { class: 'pt-fine' }, 'The key stays in this browser only. It is sent to this NineGuard server, nowhere else.')
  );

  root.replaceChildren(h('div', { class: 'auth-page' },
    themeButton('icon-btn auth-theme'),
    h('main', { class: 'auth-main' },
      h('div', { class: 'auth-card' },
        h('div', { class: 'auth-brand' }, h('img', { src: '/appicon.png', width: '34', height: '34', alt: '' }), h('span', null, 'NineGuard')),
        h('div', { class: 'auth-title' },
          h('h1', null, 'Check your key'),
          h('p', null, 'See how much of your token budget is left, when it resets, and which models you can call.')
        ),
        form
      )
    )
  ));
}

// ── Dashboard ──

function renderDashboardView() {
  const keyName = (quotaData && quotaData.key_name) || 'Your key';

  const header = h('header', { class: 'pt-top' },
    h('div', { class: 'pt-brand' },
      h('img', { src: '/appicon.png', width: '26', height: '26', alt: 'NineGuard' }),
      h('div', { class: 'pt-who' },
        h('strong', null, keyName),
        h('code', null, maskKey(activeKey))
      )
    ),
    h('div', { class: 'pt-top-actions' },
      h('button', { class: 'icon-btn', type: 'button', title: 'Reload', 'aria-label': 'Reload', disabled: loading, onclick: () => loadPortalData() }, icon('refresh')),
      themeButton('icon-btn'),
      h('button', { class: 'btn btn-sm', type: 'button', title: 'Forget this key in this browser', onclick: disconnect }, icon('logout'), h('span', { class: 'pt-hide-sm' }, 'Sign out'))
    )
  );

  const main = h('main', { class: 'pt-main' });

  if (loading && !quotaData && !usageData) {
    main.append(h('div', { class: 'pt-grid' },
      h('div', { class: 'pt-col' }, h('div', { class: 'skel', style: { height: '170px' } }), h('div', { class: 'skel', style: { height: '260px' } })),
      h('div', { class: 'pt-col' }, h('div', { class: 'skel', style: { height: '200px' } }))
    ));
    root.replaceChildren(header, main);
    return;
  }

  if (errorMsg) {
    main.append(h('section', { class: 'pt-panel pt-error' },
      icon('alert'),
      h('div', null,
        h('h2', null, 'Usage data did not load'),
        h('p', null, errorMsg),
        h('button', { class: 'btn btn-sm', type: 'button', onclick: () => loadPortalData() }, icon('refresh'), 'Try again')
      )
    ));
    root.replaceChildren(header, main);
    return;
  }

  main.append(h('div', { class: 'pt-grid' },
    h('div', { class: 'pt-col' },
      renderBudget(),
      renderActivity()
    ),
    h('aside', { class: 'pt-col' },
      renderConnect(),
      renderSharedCapacity(),
      renderAllowedModels()
    )
  ));
  root.replaceChildren(header, main);
}

function meter(pct, tone, label) {
  return h('div', {
    class: `pt-meter ${tone}`,
    role: 'meter',
    'aria-valuemin': '0',
    'aria-valuemax': '100',
    'aria-valuenow': String(Math.round(pct)),
    'aria-label': label
  }, h('i', { style: { width: `${Math.max(pct > 0 ? 1 : 0, Math.min(100, pct))}%` } }));
}

function resetLine(resetAt, period) {
  if (!resetAt) return period === 'total' ? 'Lifetime budget: it does not reset.' : '';
  const ms = Date.parse(resetAt);
  const day = new Date(ms).toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', timeZone: timeZone() || undefined });
  return `Resets in ${formatResetIn(resetAt)}, on ${day} at ${fmtClock(ms)} (${tzLabel()}).`;
}

function renderBudget() {
  const b = budgetSummary(quotaData);
  const section = h('section', { class: 'pt-panel pt-budget' });

  if (b.kind === 'unknown') {
    section.append(h('h2', { class: 'pt-status' }, 'Budget status is unavailable right now.'));
    return section;
  }
  if (b.kind === 'unlimited') {
    section.classList.add('ok');
    section.append(
      h('h2', { class: 'pt-status' }, 'This key has no token limit in force.'),
      h('p', { class: 'pt-sub' }, 'Requests are only limited by the shared upstream capacity listed on the right.'),
      ...[inactiveNote(b.inactive)].filter(Boolean)
    );
    return section;
  }

  if (b.kind === 'key') {
    section.classList.add(b.tone);
    const word = periodWord(b.period);
    const headline = b.left <= 0
      ? `Budget used up. New requests are blocked until it resets.`
      : `${formatTokensCompact(b.left)} tokens left of your ${formatTokensCompact(b.limit)} ${word} budget.`;
    section.append(
      h('h2', { class: 'pt-status' }, headline),
      meter(b.pct, b.tone, `${Math.round(b.pct)}% of ${word} budget used`),
      h('div', { class: 'pt-meter-legend' },
        h('span', null, `${fmtNum(b.used)} used`),
        h('span', null, `${Math.round(b.pct)}%`)
      ),
      h('p', { class: 'pt-sub' }, resetLine(b.resetAt, b.period))
    );
    return section;
  }

  // Per-model budgets: the combined allowance leads; each model's own bar follows.
  const tot = b.total;
  section.classList.add(tot.tone);
  const word = tot.period === 'mixed' ? '' : `${periodWord(tot.period)} `;
  const headline = b.exhausted === b.rows.length
    ? 'Every model budget is used up. Requests are blocked until they reset.'
    : `${formatTokensCompact(tot.left)} tokens left of your ${formatTokensCompact(tot.limit)} ${word}budget, across ${b.rows.length} model${b.rows.length === 1 ? '' : 's'}.`;
  const warnLine = b.exhausted
    ? `${b.exhausted === 1 ? shortModel(b.tightest.modelId) + ' is' : b.exhausted + ' models are'} used up and blocked until reset; the others keep working.`
    : (b.tightest.tone === 'warn' ? `${shortModel(b.tightest.modelId)} is close to its own limit (${Math.round(b.tightest.pct)}%).` : '');

  section.append(
    h('h2', { class: 'pt-status' }, headline),
    meter(tot.pct, tot.tone, `${Math.round(tot.pct)}% of combined budget used`),
    h('div', { class: 'pt-meter-legend' },
      h('span', null, `${fmtNum(tot.used)} used`),
      h('span', null, `${Math.round(tot.pct)}%`)
    ),
    h('p', { class: 'pt-sub' }, warnLine || 'Each model has its own limit; a used-up model is blocked while the others keep working.'),
    h('h3', { class: 'pt-mq-title' }, 'Per model'),
    h('ul', { class: 'pt-mq' }, ...b.rows.map((r) => h('li', { class: r.tone },
      h('div', { class: 'pt-mq-head' },
        h('span', { class: 'pt-mono', title: r.modelId }, r.modelId),
        h('span', { class: 'pt-num' }, `${formatTokensCompact(r.used)} / ${formatTokensCompact(r.limit)}`)
      ),
      meter(r.pct, r.tone, `${r.modelId}: ${Math.round(r.pct)}% used`),
      h('div', { class: 'pt-mq-foot' },
        h('span', null, `${periodWord(r.period)} budget`),
        h('span', null, r.resetAt ? `resets in ${formatResetIn(r.resetAt)}` : (r.period === 'total' ? 'does not reset' : ''))
      )
    ))),
    ...[inactiveNote(b.inactive)].filter(Boolean)
  );
  return section;
}

const INACTIVE_WORD = { removed: 'removed', disabled: 'turned off by the admin', no_access: 'no longer allowed' };

// Budgets the admin kept but that do not apply: listed once, quietly, so they are not mistaken for room.
function inactiveNote(list) {
  if (!list || !list.length) return null;
  return h('p', { class: 'pt-fine' },
    `Not counted: ${list.map((r) => `${shortModel(r.modelId)} (${INACTIVE_WORD[r.reason] || 'inactive'})`).join(', ')}.`);
}

function rangeSwitch() {
  return h('div', { class: 'segmented', role: 'group', 'aria-label': 'Time range' },
    ...RANGES.map(([id, label]) => h('button', {
      type: 'button',
      class: id === activeRange ? 'active' : '',
      'aria-pressed': String(id === activeRange),
      disabled: loading,
      onclick: () => {
        if (id === activeRange) return;
        activeRange = id;
        loadPortalData();
      }
    }, label))
  );
}

let activityChart = null;

function renderActivity() {
  const s = usageData;
  const pdfBtn = h('button', {
    class: 'btn btn-sm', type: 'button', disabled: loading || (!quotaData && !usageData),
    title: 'Save a summary of this key and the selected range as a PDF', onclick: printPortalSummary
  }, icon('printer'), 'Download PDF');
  const head = h('div', { class: 'pt-head' }, h('h2', null, 'Your activity'), h('div', { class: 'pt-head-actions' }, rangeSwitch(), pdfBtn));
  const section = h('section', { class: 'pt-panel' }, head);

  if (!s) {
    section.append(h('p', { class: 'pt-sub' }, 'Usage history is unavailable right now.'));
    return section;
  }
  if (!s.requests) {
    section.append(h('p', { class: 'pt-empty' }, 'No requests from this key in this period. Use the connection details to send your first one.'));
    return section;
  }

  const cur = s.cost?.currency || 'USD';
  const rate = s.cost?.rate || 1;
  const fresh = Math.max(0, (s.tokens.prompt || 0) - (s.tokens.cache_read || 0) - (s.tokens.cache_write || 0));

  const summary = activitySentence(s, activeRange);
  if (summary) {
    section.append(h('p', { class: 'pt-sub' }, summary));
  }

  const stats = [
    ['Requests', fmtNum(s.requests), s.errors ? `${fmtNum(s.errors)} failed (${s.error_rate_pct}%)` : 'none failed'],
    ['Tokens', fmtCompact(s.tokens.total), `${fmtCompact(fresh)} fresh · ${fmtCompact(s.tokens.cache_read || 0)} cache · ${fmtCompact(s.tokens.completion)} out`],
    ['Cache hits', `${s.cache_hit_pct}%`, `${fmtCompact(s.tokens.cache_read)} tokens read from cache`],
  ];
  if (s.cost) {
    stats.push(['Est. cost', s.cost.display_formatted, s.cost.unpriced_requests ? `${fmtNum(s.cost.unpriced_requests)} requests unpriced` : 'at list price']);
  } else {
    stats.push(['Active days', fmtNum(s.active_days), `busiest hour ${s.peak_hour_formatted}`]);
  }

  const statsEl = h('dl', { class: 'pt-stats' }, ...stats.map(([k, v, sub]) => h('div', null,
    h('dt', null, k), h('dd', null, v), h('small', null, sub)
  )));

  const dailyEl = renderDailyBars(s, cur, rate);
  const shareEl = renderModelShare(s, cur, rate);
  const extrasEl = renderExtras(s, cur, rate);
  const howEl = renderHowCounted(s);

  [statsEl, dailyEl, shareEl, extrasEl, howEl].filter(Boolean).forEach((el) => section.append(el));
  return section;
}

function renderDailyBars(s, cur, rate) {
  const adapted = adaptDailyTokens(s);
  if (!adapted) return null;

  activityChart?.destroy();
  activityChart = volumeChart(adapted.chartData, {
    series: adapted.series,
    height: 160,
    daily: true,
    table: true,
    segmentHover: true,
    format: fmtCompact,
    unit: 'tokens',
    errorRate: false,
    dayLabel: (i) => adapted.days[i]?.date,
  });

  return h('div', { class: 'pt-daily' },
    h('h3', null, 'Daily tokens by model'),
    activityChart.el
  );
}

function renderModelShare(s, cur, rate) {
  const models = s.models || [];
  if (!models.length) return null;
  return h('div', { class: 'pt-share' },
    h('h3', null, 'Tokens by model'),
    h('ul', null, ...models.map((m) => {
      const freshM = Math.max(0, (m.prompt || 0) - (m.cache_read || 0) - (m.cache_write || 0));
      return h('li', null,
        h('div', { class: 'pt-share-head' },
          h('span', { class: 'pt-mono', title: m.model_id }, m.model_id),
          h('span', { class: 'pt-num' },
            fmtCompact(m.total),
            (m.cost != null && s.cost) ? h('span', { class: 'pt-faint' }, ` ${formatCost(m.cost, cur, rate, 0)}`) : null
          )
        ),
        h('div', { class: 'pt-share-bar' }, h('i', { style: { width: `${Math.max(1, Math.min(100, m.pct_tokens))}%` } }), h('b', null, `${m.pct_tokens.toFixed(0)}%`)),
        h('div', { class: 'pt-faint', style: { fontSize: '11px', marginTop: '2px' } }, `${fmtCompact(freshM)} fresh · ${fmtCompact(m.cache_read || 0)} cache · ${fmtCompact(m.completion)} out`)
      );
    }))
  );
}

// Facts that only matter when non-zero, as one quiet line instead of more stat cards.
function renderExtras(s, cur, rate) {
  const parts = [];
  if (s.requests) {
    const reqToks = fmtCompact(Math.round(s.tokens.total / s.requests));
    const reqCost = s.cost ? `, ${formatCost(s.cost.usd / s.requests, cur, rate, 0)}` : '';
    let line = `Per request: ${reqToks} tokens${reqCost}`;
    if (s.sessions > 0) {
      const sessToks = fmtCompact(Math.round(s.tokens.total / s.sessions));
      const sessCost = s.cost ? `, ${formatCost(s.cost.usd / s.sessions, cur, rate, 0)}` : '';
      line += ` · Per session: ${sessToks} tokens${sessCost} · ${fmtNum(s.sessions)} sessions`;
    }
    parts.push(line);
  }

  const bits = [];
  if (s.thinking?.requested) bits.push(`thinking delivered on ${fmtNum(s.thinking.delivered)} of ${fmtNum(s.thinking.requested)} requests`);
  if (s.fallbacks) bits.push(`${fmtNum(s.fallbacks)} fallbacks to another model`);
  if (s.estimated_requests) bits.push(`${fmtNum(s.estimated_requests)} requests with estimated usage`);
  if (bits.length) parts.push(bits.join('; ') + '.');

  if (!parts.length) return null;
  return h('p', { class: 'pt-extras' }, parts.join(' · '));
}

function renderHowCounted(s) {
  const tz = s.timezone || 'UTC';
  return h('details', { class: 'pt-how' },
    h('summary', null, 'How these numbers are counted'),
    h('ul', null,
      h('li', null, `Dates and times use your time zone (${tz}).`),
      h('li', null, 'Tokens = input + output. Input includes cache reads; "fresh" input excludes them.'),
      h('li', null, 'Cost is an estimate at list price; requests without a known price are not included.'),
      h('li', null, 'Failed requests are not counted in tokens or cost.')
    )
  );
}

// ── Print / PDF summary ──
// Built only from data already loaded; the key itself appears masked, never raw.

function localDateStr(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function printTable(columns, rows) {
  return h('table', { class: 'print-table' },
    h('thead', null, h('tr', null, ...columns.map(([label, num]) => h('th', { class: num ? 'num' : null }, label)))),
    h('tbody', null, ...rows.map((cells) => h('tr', null,
      ...cells.map((v, i) => h('td', { class: columns[i][1] ? 'num' : null }, v))
    )))
  );
}

function printSection(title, ...children) {
  return h('div', { class: 'print-section' }, h('h2', { class: 'print-section-title' }, title), ...children);
}

function printBudgetSection() {
  const b = budgetSummary(quotaData);
  if (b.kind === 'unknown') return printSection('Token budget', h('p', { class: 'print-text' }, 'Budget status was unavailable when this summary was made.'));
  const inactive = b.inactive && b.inactive.length
    ? h('p', { class: 'print-note' }, `Not counted: ${b.inactive.map((r) => `${r.modelId} (${INACTIVE_WORD[r.reason] || 'inactive'})`).join(', ')}.`)
    : null;
  if (b.kind === 'unlimited') return printSection('Token budget', h('p', { class: 'print-text' }, 'This key has no token limit in force.'), inactive);

  const pct = (p) => `${Math.round(p)}%`;
  if (b.kind === 'key') {
    return printSection('Token budget',
      printTable([['Period'], ['Used', true], ['Limit', true], ['Left', true], ['Used %', true]],
        [[periodWord(b.period) || '-', fmtNum(b.used), fmtNum(b.limit), fmtNum(b.left), pct(b.pct)]]),
      h('p', { class: 'print-note' }, resetLine(b.resetAt, b.period))
    );
  }
  const tot = b.total;
  return printSection('Token budget',
    h('p', { class: 'print-text' }, `${fmtNum(tot.left)} tokens left of ${fmtNum(tot.limit)} across ${b.rows.length} model budget${b.rows.length === 1 ? '' : 's'} (${pct(tot.pct)} used).`),
    printTable([['Model'], ['Period'], ['Used', true], ['Limit', true], ['Used %', true], ['Resets']],
      b.rows.map((r) => [r.modelId, periodWord(r.period) || '-', fmtNum(r.used), fmtNum(r.limit), pct(r.pct),
        r.resetAt ? `in ${formatResetIn(r.resetAt)}` : (r.period === 'total' ? 'never' : '-')])),
    inactive
  );
}

function renderPortalPrint() {
  const s = usageData;
  const keyName = (quotaData && quotaData.key_name) || 'Your key';
  const rangeLabel = (RANGES.find(([id]) => id === activeRange) || [, activeRange])[1];
  const rangeText = s && s.from && s.to ? `${rangeLabel} (${s.from} to ${s.to})` : rangeLabel;
  const cur = s?.cost?.currency || 'USD';
  const rate = s?.cost?.rate || 1;

  const head = h('div', { class: 'print-report-header' },
    h('div', null,
      h('div', { class: 'brand-row' },
        h('img', { src: '/appicon.png', width: '28', height: '28', alt: 'NineGuard' }),
        h('span', { class: 'brand-title' }, 'NineGuard')
      ),
      h('h1', { class: 'report-title' }, `Key usage summary: ${keyName}`),
      h('p', { class: 'report-period' }, `Range: ${rangeText} · Timezone: ${tzLabel()}`)
    ),
    h('div', { class: 'header-right' },
      h('div', null, 'Key: ', h('b', null, maskKey(activeKey))),
      h('div', null, 'Exported: ', h('b', null, new Date().toLocaleString('en-US', { timeZone: timeZone() || undefined }))),
      s?.cost ? h('div', null, 'Currency: ', h('b', null, cur)) : null
    )
  );

  const parts = [head, printBudgetSection()];

  if (!s) {
    parts.push(printSection('Usage', h('p', { class: 'print-text' }, 'Usage history was unavailable when this summary was made.')));
  } else if (!s.requests) {
    parts.push(printSection('Usage', h('p', { class: 'print-text' }, 'No requests from this key in this range.')));
  } else {
    const freshTotal = Math.max(0, (s.tokens.prompt || 0) - (s.tokens.cache_read || 0) - (s.tokens.cache_write || 0));
    const kpi = (label, value, sub) => h('div', { class: 'print-kpi-card' },
      h('div', { class: 'label' }, label), h('div', { class: 'value' }, value), h('div', { class: 'sub' }, sub));
    parts.push(h('div', { class: 'print-kpi-grid' },
      kpi('Requests', fmtNum(s.requests), s.errors ? `${fmtNum(s.errors)} failed (${s.error_rate_pct}%)` : 'none failed'),
      kpi('Tokens', fmtNum(s.tokens.total), `${fmtNum(freshTotal)} fresh in, ${fmtNum(s.tokens.cache_read || 0)} from cache, ${fmtNum(s.tokens.completion)} out`),
      kpi('Cache hits', `${s.cache_hit_pct}%`, `${fmtNum(s.tokens.cache_read)} tokens read from cache`),
      s.cost
        ? kpi('Est. cost', s.cost.display_formatted, s.cost.unpriced_requests ? `${fmtNum(s.cost.unpriced_requests)} requests unpriced` : 'at list price')
        : kpi('Active days', fmtNum(s.active_days), `busiest hour ${s.peak_hour_formatted}`)
    ));
    const extras = renderExtras(s);
    if (extras) parts.push(h('p', { class: 'print-text' }, extras.textContent));

    const models = s.models || [];
    if (models.length) {
      const cols = [['Model'], ['Fresh input', true], ['Cache read', true], ['Output', true], ['Total', true], ['Share', true]];
      if (s.cost) cols.push(['Est. cost', true]);
      parts.push(printSection('Tokens by model',
        printTable(cols, models.map((m) => {
          const freshM = Math.max(0, (m.prompt || 0) - (m.cache_read || 0) - (m.cache_write || 0));
          const row = [m.model_id, fmtNum(freshM), fmtNum(m.cache_read || 0), fmtNum(m.completion), fmtNum(m.total), `${(m.pct_tokens || 0).toFixed(1)}%`];
          if (s.cost) row.push(m.cost != null ? formatCost(m.cost, cur, rate, 0) : '-');
          return row;
        })),
        h('p', { class: 'print-note' }, "Cache reads are prompt tokens served from the provider's cache and are billed at a reduced rate.")
      ));
    }

    const days = (s.daily || []).filter((d) => d.requests || d.tokens);
    if (days.length) {
      const cols = [['Date'], ['Requests', true], ['Tokens', true]];
      if (s.cost) cols.push(['Est. cost', true]);
      parts.push(printSection('Daily usage (active days)', printTable(cols, days.map((d) => {
        const row = [d.date, fmtNum(d.requests), fmtNum(d.tokens)];
        if (s.cost) row.push(d.cost != null ? formatCost(d.cost, cur, rate, 0) : '-');
        return row;
      }))));
    }
  }

  parts.push(h('div', { class: 'print-footer' },
    h('span', null, 'Generated by the NineGuard key portal'),
    h('span', null, `Range: ${rangeText}`)
  ));
  return h('div', { class: 'print-doc' }, ...parts);
}

function printPortalSummary() {
  if (!quotaData && !usageData) return;
  document.querySelectorAll('.print-doc').forEach((n) => n.remove());
  const doc = renderPortalPrint();
  document.body.append(doc);

  const keyName = (quotaData && quotaData.key_name) || 'key';
  const slug = keyName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'key';
  const prevTitle = document.title;
  document.title = `NineGuard-usage-${slug}-${activeRange}-${localDateStr(new Date())}`;
  let restored = false;
  const restore = () => {
    if (restored) return;
    restored = true;
    window.removeEventListener('afterprint', restore);
    doc.remove();
    document.title = prevTitle;
  };
  window.addEventListener('afterprint', restore);

  Promise.all([...doc.querySelectorAll('img')].map((img) => img.decode().catch(() => {}))).then(() => window.print());
}

function copyRow(label, value, hint) {
  return h('div', { class: 'pt-copy' },
    h('div', { class: 'pt-copy-text' },
      h('span', null, label),
      h('code', null, value),
      hint ? h('small', null, hint) : null
    ),
    h('button', { class: 'icon-btn', type: 'button', title: `Copy ${label}`, 'aria-label': `Copy ${label}`, onclick: () => copy(value, `${label} copied`) }, icon('copy'))
  );
}

function renderConnect() {
  const origin = location.origin;
  return h('section', { class: 'pt-panel' },
    h('h2', null, 'Connect'),
    copyRow('OpenAI-compatible base URL', `${origin}/v1`, 'Cursor, Cline, Continue, OpenAI SDK'),
    copyRow('Anthropic base URL', origin, 'Claude Code, Anthropic SDK. Recommended for Claude models.'),
    h('p', { class: 'pt-fine' }, 'Use your key as the API key. Model ids include the provider prefix, as listed below.')
  );
}

function renderSharedBucket(b) {
  if (isPercentBucket(b)) {
    const m = sdkMeter(b);
    return h('li', { class: m.dim ? 'is-dim' : null },
      h('div', { class: 'pt-mq-head' },
        h('span', null, b.name),
        h('span', { class: `pt-num tone-${m.tone}` },
          remainingLabel(b),
          b.reset_at ? h('span', { class: 'pt-reset', title: `resets in ${formatResetIn(b.reset_at)}` }, ` · ${formatResetIn(b.reset_at)}`) : null
        )
      ),
      meter(m.pct, m.known ? m.tone : 'muted', `${b.name}: ${remainingLabel(b)}`),
      m.dim ? h('p', { class: 'pt-fine' }, `${observationLabel(b.observed_at, b.observation_state)}${b.incomplete ? ' · incomplete observations' : ''}`) : null
    );
  }
  if (b.is_credit_balance) {
    return h('li', null, h('div', { class: 'pt-mq-head' }, h('span', null, b.name), h('span', { class: 'pt-num' }, `$${(b.remaining / 100).toFixed(2)} credit`)));
  }
  const left = Math.max(0, Math.min(100, b.remaining_percentage || 0));
  const tone = remainingTone(left);
  return h('li', null,
    h('div', { class: 'pt-mq-head' },
      h('span', null, b.name),
      h('span', { class: `pt-num tone-${tone}` },
        left ? `${left}% left` : 'Limit reached',
        b.reset_at ? h('span', { class: 'pt-reset', title: `resets in ${formatResetIn(b.reset_at)}` }, ` · ${formatResetIn(b.reset_at)}`) : null
      )
    ),
    meter(left, tone, `${b.name}: ${left}% left`)
  );
}

function renderSharedCapacity() {
  const pools = (quotaData && quotaData.upstream_pools) || [];
  if (!pools.length) return null;

  return h('section', { class: 'pt-panel' },
    h('h2', null, 'Shared capacity'),
    h('p', { class: 'pt-sub' }, 'Upstream limits shared by every key. When one runs out, requests fail even if your own budget is left.'),
    h('ul', { class: 'pt-pools' }, ...pools.map((pool) => {
      const buckets = (pool.buckets || []).filter((b) => isPercentBucket(b) || b.is_credit_balance || b.total > 0);
      const name = pool.family || pool.provider;
      const poolKey = `${pool.provider}|${pool.sub_provider}|${pool.family}`;

      if (!buckets.length) {
        return h('li', { class: `pt-pool${pool.status === 'error' ? ' is-err' : pool.status === 'degraded' ? ' is-warn' : ''}` },
          h('div', { class: 'pt-pool-head' },
            h('span', { class: 'pt-pool-name' }, iconTile({ img: subProviderIconURL(pool.sub_provider) }, name, 'sm'), h('strong', null, name)),
            pool.status && pool.status !== 'ok' ? h('span', { class: `badge ${pool.status === 'degraded' ? 'warn' : 'err'}` }, pool.status) : null
          ),
          pool.source === 'sdk' ? h('p', { class: 'pt-fine' }, 'Most constrained observed account; not pooled capacity. Other connected accounts may still have room.') : null,
          h('p', { class: 'pt-fine' }, pool.source === 'sdk' ? telemetryStateLabel(pool.observation_state) : 'No limits reported.')
        );
      }

      const { shown, hidden, minHiddenLeft } = splitPoolBuckets(buckets);
      const allBuckets = [...shown, ...hidden];
      const listEl = h('ul', { class: 'pt-buckets' });
      let moreBtn = null;

      function paintBuckets() {
        const isExpanded = expandedPools.has(poolKey);
        const displayBuckets = isExpanded ? allBuckets : shown;
        listEl.replaceChildren(...displayBuckets.map(renderSharedBucket));
        if (moreBtn) {
          const suffix = minHiddenLeft === 0
            ? ' · some at limit'
            : minHiddenLeft != null
              ? ` · all ≥${minHiddenLeft}% left`
              : '';
          const collapsedText = `+${hidden.length} more${suffix}`;
          moreBtn.textContent = isExpanded ? 'Show fewer' : collapsedText;
        }
      }

      if (hidden.length > 0) {
        moreBtn = h('button', { class: 'btn btn-sm pt-more', type: 'button' });
        moreBtn.addEventListener('click', () => {
          if (expandedPools.has(poolKey)) {
            expandedPools.delete(poolKey);
          } else {
            expandedPools.add(poolKey);
          }
          paintBuckets();
        });
      }

      paintBuckets();

      return h('li', { class: `pt-pool${pool.status === 'error' ? ' is-err' : pool.status === 'degraded' ? ' is-warn' : ''}` },
        h('div', { class: 'pt-pool-head' },
          h('span', { class: 'pt-pool-name' }, iconTile({ img: subProviderIconURL(pool.sub_provider) }, name, 'sm'), h('strong', null, name)),
          pool.status && pool.status !== 'ok' ? h('span', { class: `badge ${pool.status === 'degraded' ? 'warn' : 'err'}` }, pool.status) : null
        ),
        pool.source === 'sdk' ? h('p', { class: 'pt-fine' }, 'Most constrained observed account; not pooled capacity. Other connected accounts may still have room.') : null,
        listEl,
        moreBtn
      );
    }))
  );
}

function renderAllowedModels() {
  const list = modelsData || [];
  const section = h('section', { class: 'pt-panel' });
  const listEl = h('ul', { class: 'pt-models' });
  const more = h('button', { class: 'btn btn-sm pt-more', type: 'button' });

  function paint() {
    const q = modelFilter.toLowerCase();
    const hits = list.filter((m) => !q || m.id.toLowerCase().includes(q));
    const shown = (showAllModels || q) ? hits : hits.slice(0, MODEL_LIST_PREVIEW);
    listEl.replaceChildren(...(shown.length
      ? shown.map((m) => h('li', null,
          h('button', { type: 'button', title: 'Copy model id', onclick: () => copy(m.id, `${m.id} copied`) },
            h('span', { class: 'pt-mono' }, m.id), icon('copy'))
        ))
      : [h('li', { class: 'pt-fine' }, `No model matches "${modelFilter}".`)]));
    more.hidden = !!q || hits.length <= MODEL_LIST_PREVIEW;
    more.textContent = showAllModels ? 'Show fewer' : `Show all ${hits.length}`;
  }
  more.addEventListener('click', () => { showAllModels = !showAllModels; paint(); });

  section.append(h('div', { class: 'pt-head' }, h('h2', null, 'Models you can call'), h('span', { class: 'pt-faint' }, fmtNum(list.length))));
  if (!list.length) {
    section.append(h('p', { class: 'pt-sub' }, 'No models are enabled for this key. Ask your NineGuard admin to grant access.'));
    return section;
  }
  if (list.length > MODEL_LIST_PREVIEW) {
    const input = h('input', {
      type: 'search', placeholder: 'Filter models...', autocomplete: 'off', spellcheck: 'false', 'aria-label': 'Filter models',
      value: modelFilter,
      oninput: (e) => { modelFilter = e.target.value.trim(); paint(); }
    });
    section.append(h('label', { class: 'tool-search pt-filter' }, icon('search'), input));
  }
  section.append(listEl, more);
  paint();
  return section;
}

function render() {
  if (!activeKey) renderLoginView();
  else renderDashboardView();
}

if (activeKey) loadPortalData();
else render();
