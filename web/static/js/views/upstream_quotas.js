// Upstream Quotas view: Live visibility into upstream provider capacity, rolling limits, and credit balances.
import { api } from '../api.js';
import { h, icon, toast, emptyState, confirmDialog } from '../ui.js';
import { formatResetIn } from '../modelquotas.js';
import { formatTokensCompact } from '../quotahelpers.js';
import { providerMark, iconTile, subProviderTile } from '../providericons.js';
import { isPercentBucket, remainingLabel, observationLabel, telemetryStateLabel, freshPercent, remainingTone, sdkMeter } from '../upstreamtelemetry.js';

export const upstreamBarTone = remainingTone;

export function upstreamAlertRows(providers, max = 6) {
  if (!Array.isArray(providers)) return { rows: [], total: 0 };

  const errorProblemRows = [];
  const degradedProblemRows = [];
  const bucketRows = [];

  for (const p of providers) {
    if (!p) continue;
    if (!p.quota_type || p.quota_type === 'disabled') continue;
    if (p.status === 'unsupported') continue;

    const providerName = p.provider_name || p.provider_id || '';
    const connections = Array.isArray(p.connections) ? p.connections : [];

    if (connections.length === 0) {
      if (p.status === 'error') {
        errorProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel: '',
          status: 'error',
          errorMsg: p.error_msg || '',
          tone: 'err',
        });
      } else if (p.status === 'degraded') {
        degradedProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel: '',
          status: 'degraded',
          errorMsg: p.error_msg || '',
          tone: 'warn',
        });
      }
      continue;
    }

    for (const conn of connections) {
      if (!conn) continue;
      const accountLabel = conn.account_label || '';

      if (conn.status === 'error') {
        errorProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel,
          status: 'error',
          errorMsg: conn.error_msg || '',
          tone: 'err',
        });
      } else if (conn.status === 'degraded') {
        degradedProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel,
          status: 'degraded',
          errorMsg: conn.error_msg || '',
          tone: 'warn',
        });
      }

      const buckets = Array.isArray(conn.buckets) ? conn.buckets : [];
      for (const b of buckets) {
        if (!b) continue;
        if (isPercentBucket(b) && !freshPercent(b)) continue;
        if (b.warning === true) {
          const isCredit = Boolean(b.is_credit_balance);
          const remaining = typeof b.remaining === 'number' ? b.remaining : 0;
          const rawPct = isPercentBucket(b) ? (b.remaining_percent ?? 0) : (typeof b.remaining_percentage === 'number' ? b.remaining_percentage : 0);
          const percent = Math.max(0, Math.min(100, Math.round(rawPct)));
          let tone = upstreamBarTone(percent);
          if (isCredit && remaining <= 0) {
            tone = 'err';
          }
          bucketRows.push({
            kind: 'bucket',
            providerName,
            accountLabel,
            bucketName: b.name || '',
            percent,
            isCredit,
            remaining,
            resetAt: b.reset_at || null,
            tone,
          });
        }
      }
    }
  }

  bucketRows.sort((a, b) => {
    const effA = a.isCredit && a.remaining <= 0 ? 0 : a.percent;
    const effB = b.isCredit && b.remaining <= 0 ? 0 : b.percent;
    return effA - effB;
  });

  const allRows = [...errorProblemRows, ...bucketRows, ...degradedProblemRows];
  const total = allRows.length;
  const limit = typeof max === 'number' && max >= 0 ? max : 6;
  const rows = allRows.slice(0, limit);

  return { rows, total };
}

function timeAgo(dateStr) {
  if (!dateStr) return '';
  const diffSec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (diffSec < 60) return 'just now';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return `${Math.floor(diffSec / 86400)}d ago`;
}

// True when a provider needs a look: provider/connection errors or degradation, or a warning bucket.
export function upstreamNeedsAttention(p) {
  if (!p) return false;
  if (p.status === 'error' || p.status === 'degraded' || p.error_msg) return true;
  return (p.connections || []).some((c) => c && (c.status === 'error' || c.status === 'degraded'
    || (c.buckets || []).some((b) => b && b.warning === true)));
}

// One-line digest for a collapsed provider card: connections, lowest remaining %, soonest reset.
export function upstreamProviderSummary(p, now = Date.now()) {
  const conns = (p?.connections || []).filter(Boolean);
  let low = null;
  let reset = null;
  for (const c of conns) {
    for (const b of c.buckets || []) {
      if (!b) continue;
      let pct = null;
      if (isPercentBucket(b)) { if (freshPercent(b, now) && Number.isFinite(b.remaining_percent)) pct = b.remaining_percent; }
      else if (!b.is_credit_balance && b.total > 0 && Number.isFinite(b.remaining_percentage)) pct = b.remaining_percentage;
      if (pct != null) low = low == null ? pct : Math.min(low, pct);
      const t = b.reset_at ? Date.parse(b.reset_at) : NaN;
      if (Number.isFinite(t) && t > now && (reset == null || t < reset)) reset = t;
    }
  }
  const parts = [`${conns.length} ${conns.length === 1 ? 'connection' : 'connections'}`];
  if (low != null) parts.push(`lowest ${Math.max(0, Math.min(100, Math.round(low)))}% left`);
  if (reset != null) parts.push(`next reset in ${formatResetIn(new Date(reset).toISOString())}`);
  return parts.join(' · ');
}

// User choices per provider id (true = collapsed); unset providers use the default.
const COLLAPSE_KEY = 'ng.upstream.collapsed';
function loadCollapsed() {
  try { return JSON.parse(localStorage.getItem(COLLAPSE_KEY) || '{}') || {}; } catch { return {}; }
}
const providerKey = (p) => String(p.provider_id || p.provider_name || '');
function isProviderOpen(p, count) {
  const saved = loadCollapsed()[providerKey(p)];
  if (typeof saved === 'boolean') return !saved;
  return count <= 2 || upstreamNeedsAttention(p);
}
function setProviderOpen(p, open) {
  const all = loadCollapsed();
  all[providerKey(p)] = !open;
  try { localStorage.setItem(COLLAPSE_KEY, JSON.stringify(all)); } catch { /* storage unavailable */ }
}

// Group-by choice: 'account' (one card per connection), 'provider' (one card per sub-provider), or 'model' (one card per model family).
const GROUP_KEY = 'ng.upstream.groupBy';
const VALID_GROUP_BY = new Set(['account', 'provider', 'model']);
export function loadGroupBy() {
  try {
    const val = localStorage.getItem(GROUP_KEY);
    return VALID_GROUP_BY.has(val) ? val : 'account';
  } catch {
    return 'account';
  }
}
function saveGroupBy(v) {
  try { localStorage.setItem(GROUP_KEY, v); } catch { /* storage unavailable */ }
}

// ---- Model families -------------------------------------------------------
export const FAMILY_ORDER = ['allowance', 'claude', 'gemini', 'gpt', 'other', 'credit'];
export const FAMILY_META = {
  allowance: { title: 'Key allowance', spec: { ic: 'shield' } },
  claude: { title: 'Claude', spec: { img: '/provider-icons/claude.svg' } },
  gemini: { title: 'Gemini', spec: { img: '/provider-icons/gemini.svg' } },
  gpt: { title: 'GPT / OpenAI', spec: { img: '/provider-icons/openai.svg' } },
  other: { title: 'Other / account-wide windows', spec: { ic: 'clock' } },
  credit: { title: 'Credits', spec: { ic: 'dollar-sign' } },
};
// Single-vendor connections may claim generic windows ("5h Session"); antigravity/github host several vendors, so they do not.
const PROVIDER_FAMILY = { claude: 'claude', anthropic: 'claude', codex: 'gpt', openai: 'gpt', gemini: 'gemini', google: 'gemini', 'gemini-cli': 'gemini' };

// Family key for one bucket of a connection. Order matters: 9router's "Claude & GPT (5h)" pools count as Claude.
export function bucketFamily(b, conn) {
  if (b?.is_credit_balance) return 'credit';
  const name = String(b?.name || '');
  if (/^key allowance/i.test(name)) return 'allowance';
  const text = `${b?.scope && b.scope !== 'account' ? b.scope : ''} ${name}`; // SDK per-model buckets carry the model id in scope
  if (/claude|sonnet|opus|haiku/i.test(text)) return 'claude';
  if (/gemini|imagen/i.test(text)) return 'gemini';
  if (/gpt|codex|\bo[134]\b|openai/i.test(text)) return 'gpt';
  return PROVIDER_FAMILY[String(conn?.provider || '').toLowerCase()] || 'other';
}

// Provider -> { cards: [{ family, groups: [{ conn, buckets }] }], attention: [conn] }.
// Errored or empty connections also land in `attention` (errored ones keep their buckets in the family cards).
export function groupProviderByFamily(provider) {
  const byFamily = {};
  const attention = [];
  for (const conn of (provider?.connections || [])) {
    if (!conn) continue;
    const buckets = Array.isArray(conn.buckets) ? conn.buckets.filter(Boolean) : [];
    if (conn.status === 'error' || (conn.status === 'degraded' && conn.error_msg) || !buckets.length) attention.push(conn);
    const per = {};
    for (const b of buckets) (per[bucketFamily(b, conn)] ||= []).push(b);
    for (const [family, bs] of Object.entries(per)) (byFamily[family] ||= []).push({ conn, buckets: bs });
  }
  const cards = FAMILY_ORDER.filter((f) => byFamily[f]).map((family) => ({ family, groups: byFamily[family] }));
  return { cards, attention };
}

// Provider -> { cards: [{ subProvider, groups: [{ conn, buckets }] }], attention: [conn] }.
// Errored or empty connections also land in `attention` (errored ones keep their buckets in the sub-provider cards).
export function groupProviderBySubProvider(provider) {
  const cards = [];
  const bySubProvider = new Map();
  const attention = [];
  for (const conn of (provider?.connections || [])) {
    if (!conn) continue;
    const buckets = Array.isArray(conn.buckets) ? conn.buckets.filter(Boolean) : [];
    if (conn.status === 'error' || (conn.status === 'degraded' && conn.error_msg) || !buckets.length) attention.push(conn);
    if (!buckets.length) continue;
    const subProvider = String(conn.provider || '').toLowerCase();
    let entry = bySubProvider.get(subProvider);
    if (!entry) {
      entry = { subProvider, groups: [] };
      bySubProvider.set(subProvider, entry);
      cards.push(entry);
    }
    const sorted = [...buckets].sort((a, b) => FAMILY_ORDER.indexOf(bucketFamily(a, conn)) - FAMILY_ORDER.indexOf(bucketFamily(b, conn)));
    entry.groups.push({ conn, buckets: sorted });
  }
  return { cards, attention };
}

// Display model for one bucket, built on the shared telemetry helpers.
export function bucketMeter(b) {
  if (isPercentBucket(b)) {
    const m = sdkMeter(b);
    const limit = !!b.limit_reached || (m.known && m.pct <= 0);
    return { pct: m.pct, tone: limit ? 'err' : m.tone, dim: m.dim, limit, label: limit ? 'Limit reached' : remainingLabel(b) };
  }
  const pct = Math.round(Math.max(0, Math.min(100, b?.remaining_percentage || 0)) * 10) / 10;
  const limit = pct === 0 && b?.total > 0;
  return { pct, tone: upstreamBarTone(pct), dim: false, limit, label: limit ? 'Limit reached' : `${pct}% left` };
}

// "claude-sonnet-5-5 (daily)" -> name + period chip.
export function splitPeriod(name) {
  const m = /^(.*) \((daily|weekly|monthly|total)\)$/.exec(String(name || ''));
  return m ? { name: m[1], period: m[2] } : { name: String(name || ''), period: null };
}

// Worst state across buckets: limit reached (or empty credit) and the lowest known remaining %.
function worstOf(buckets) {
  let limit = false;
  let low = null;
  for (const b of buckets) {
    if (b.is_credit_balance) { if (b.remaining <= 0) limit = true; continue; }
    const m = bucketMeter(b);
    if (m.limit) limit = true;
    else if (!m.dim) low = low == null ? m.pct : Math.min(low, m.pct);
  }
  return { limit, low };
}
const cardState = (w) => (w.limit ? ' is-err' : w.low != null && w.low < 30 ? ' is-warn' : '');
const isAllowanceConn = (c) => (c.connection_id || '').includes('allowance');
const isSharedPool = (c, p) => !isAllowanceConn(c) && (p.quota_type === 'nineguard' || c.plan === 'Shared Upstream Pool');
const planOf = (c) => (c.plan && c.plan !== 'Shared Upstream Pool' ? c.plan : null);

const MODEL_THEME = {
  claude: 'th-claude',
  gemini: 'th-gemini',
  gpt: 'th-gpt',
};

const SUB_PROVIDER_THEME = {
  claude: 'th-claude',
  anthropic: 'th-claude',
  gemini: 'th-gemini',
  google: 'th-gemini',
  'gemini-cli': 'th-gemini',
  codex: 'th-gpt',
  openai: 'th-gpt',
  antigravity: 'th-ag',
};

export function bentoSpan(cardCount, totalCols, hasSmallGroups) {
  const n = cardCount;
  let span = Math.min(n, totalCols);
  if (totalCols >= 4 && hasSmallGroups && n >= 4) {
    if (n === 6 || n === 5) span = 3;
  } else if (totalCols >= 4 && n === 4) {
    span = 2;
  }
  return Math.max(1, span);
}

export function layoutBento(stageEl) {
  if (!stageEl || !stageEl.clientWidth) return;

  const MIN_COL = 300;
  const GAP = 16;
  const isNarrow = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(max-width: 560px)').matches;

  if (stageEl.classList.contains('gb-acct-stage')) {
    if (!stageEl._origCards) {
      stageEl._origCards = Array.from(stageEl.children);
    }
    const cards = stageEl._origCards;
    const W = stageEl.clientWidth;
    let numCols = isNarrow ? 1 : Math.max(1, Math.floor((W + 14) / (MIN_COL + 14)));
    numCols = Math.min(numCols, Math.max(1, cards.length));

    stageEl.replaceChildren();
    const wrap = document.createElement('div');
    wrap.className = 'gb-acct-masonry';
    const cols = [];
    const colHeights = [];
    for (let i = 0; i < numCols; i++) {
      const c = document.createElement('div');
      c.className = 'gb-acct-col';
      wrap.appendChild(c);
      cols.push(c);
      colHeights.push(0);
    }
    stageEl.appendChild(wrap);

    cards.forEach((card) => {
      let best = 0;
      for (let k = 1; k < numCols; k++) {
        if (colHeights[k] < colHeights[best]) best = k;
      }
      cols[best].appendChild(card);
      colHeights[best] += (card.getBoundingClientRect?.()?.height || 0) + 14;
    });
    return;
  }

  // Grouped views (.gb-stage)
  if (!stageEl._origGroups) {
    stageEl._origGroups = Array.from(stageEl.children).map((box) => {
      const body = box.querySelector('.gb-body');
      return {
        box,
        body,
        cards: body ? Array.from(body.children) : []
      };
    });
  }
  const groups = stageEl._origGroups;
  const W = stageEl.clientWidth;
  const totalCols = isNarrow ? 1 : Math.max(1, Math.floor((W + GAP) / (MIN_COL + GAP)));

  const hasSmallGroups = groups.some((g) => g.cards.length === 1);
  groups.forEach((g) => {
    g.span = bentoSpan(g.cards.length, totalCols, hasSmallGroups);
  });

  function buildGroupInner(g) {
    if (!g.body) return;
    const n = g.cards.length;
    const s = Math.max(1, Math.min(g.span, n));
    g.body.replaceChildren();

    if (s === 1 || n === 1) {
      const singleCol = document.createElement('div');
      singleCol.className = 'gb-inner-cols';
      const colEl = document.createElement('div');
      colEl.className = 'gb-inner-col';
      g.cards.forEach((c) => { colEl.appendChild(c); });
      singleCol.appendChild(colEl);
      g.body.appendChild(singleCol);
      return;
    }

    const rem = n % s;
    if (rem === 0) {
      const colsWrap = document.createElement('div');
      colsWrap.className = 'gb-inner-cols';
      const innerCols = [];
      const innerH = [];
      for (let i = 0; i < s; i++) {
        const ic = document.createElement('div');
        ic.className = 'gb-inner-col';
        colsWrap.appendChild(ic);
        innerCols.push(ic);
        innerH.push(0);
      }
      g.body.appendChild(colsWrap);
      g.cards.forEach((c) => {
        let best = 0;
        for (let k = 1; k < s; k++) {
          if (innerH[k] < innerH[best]) best = k;
        }
        innerCols[best].appendChild(c);
        innerH[best] += (c.getBoundingClientRect?.()?.height || 0) + 14;
      });
    } else {
      const gridWrap = document.createElement('div');
      gridWrap.className = 'gb-inner-grid';
      const fullRowsCount = n - rem;
      g.cards.forEach((c, idx) => {
        const span12 = idx < fullRowsCount ? Math.floor(12 / s) : Math.floor(12 / rem);
        c.style.gridColumn = `span ${span12}`;
        gridWrap.appendChild(c);
      });
      g.body.appendChild(gridWrap);
    }
  }

  stageEl.replaceChildren();
  const board = document.createElement('div');
  board.className = 'gb-board';
  stageEl.appendChild(board);

  const wideGroups = groups.filter((g) => g.span > 1);
  const unitGroups = groups.filter((g) => g.span === 1);

  function placeInColumns(targetGroups, colCount) {
    const cols = [];
    const colH = [];
    for (let i = 0; i < colCount; i++) {
      const col = document.createElement('div');
      col.className = 'gb-board-col';
      col.style.flex = '1 1 0%';
      board.appendChild(col);
      cols.push(col);
      colH.push(0);
    }

    targetGroups.forEach((g) => {
      let best = 0;
      for (let k = 1; k < cols.length; k++) {
        if (colH[k] < colH[best]) best = k;
      }
      cols[best].appendChild(g.box);
      buildGroupInner(g);
      colH[best] += (g.box.getBoundingClientRect?.()?.height || 0) + GAP;
    });
  }

  if (wideGroups.length && unitGroups.length && totalCols >= 3) {
    const maxWideSpan = Math.min(totalCols - 1, Math.max(...wideGroups.map((g) => g.span)));
    const sideSpan = Math.max(1, Math.min(totalCols - maxWideSpan, unitGroups.length));

    const mainCol = document.createElement('div');
    mainCol.className = 'gb-board-col';
    mainCol.style.flex = `${maxWideSpan} 1 0%`;
    board.appendChild(mainCol);

    wideGroups.forEach((g) => {
      g.span = maxWideSpan;
      mainCol.appendChild(g.box);
      buildGroupInner(g);
    });

    placeInColumns(unitGroups, sideSpan);
  } else if (wideGroups.length === 0 && totalCols > 1) {
    placeInColumns(groups, Math.min(totalCols, groups.length));
  } else {
    const mainStack = document.createElement('div');
    mainStack.className = 'gb-board-col';
    mainStack.style.flex = '1 1 100%';
    board.appendChild(mainStack);
    groups.forEach((g) => {
      mainStack.appendChild(g.box);
      buildGroupInner(g);
    });
  }
}

// ---- View -----------------------------------------------------------------
export function renderUpstreamQuotas(container) {
  let alive = true;
  let loading = false;
  let loaded = false;
  let providersData = [];
  let groupBy = loadGroupBy();
  let cooldownUntil = 0;
  let cooldownTimer = null;
  let stageObservers = [];

  const content = h('div', { class: 'mt' },
    h('div', { class: 'skel', style: { height: '140px', borderRadius: '8px' } })
  );

  // Auto-sync interval setting (global, applies to all providers)
  const intervalSel = h('select', { class: 'select', disabled: true });
  const syncBox = h('div', { class: 'uq-toolbar-sync' },
    icon('refresh'),
    h('label', { style: { fontSize: '12.5px', fontWeight: '600' } }, 'Auto-sync every'),
    intervalSel,
    h('span', { class: 'muted', style: { fontSize: '11.5px' } }, 'Applies to the dashboard and the agent /quota command.')
  );
  const groupBtns = {};
  const groupOptions = [
    ['account', 'Account'],
    ['provider', 'Provider'],
    ['model', 'Model'],
  ];
  for (const [value, label] of groupOptions) {
    groupBtns[value] = h('button', {
      type: 'button', 'aria-pressed': String(groupBy === value),
      onclick: () => {
        if (groupBy === value) return;
        groupBy = value;
        saveGroupBy(value);
        for (const [v, b] of Object.entries(groupBtns)) b.setAttribute('aria-pressed', String(groupBy === v));
        if (loaded) render();
      },
    }, label);
  }
  const toolbar = h('div', { class: 'uq-toolbar' },
    syncBox,
    h('div', { class: 'uq-toolbar-group' },
      h('span', { class: 'muted' }, 'Group by'),
      h('div', { class: 'segmented uq-seg', role: 'group', 'aria-label': 'Group by' },
        groupBtns.account, groupBtns.provider, groupBtns.model
      )
    )
  );
  container.replaceChildren(toolbar, content);

  async function loadSyncSettings() {
    try {
      const s = await api.get('/settings/upstream-sync');
      if (!alive) return;
      intervalSel.replaceChildren(...(s.allowed || []).map((v) =>
        h('option', { value: v, selected: v === s.interval }, v)
      ));
      intervalSel.value = s.interval;
      intervalSel.disabled = false;
    } catch (err) {
      if (!alive) return;
      syncBox.replaceChildren(
        h('span', { class: 'muted', style: { fontSize: '12px' } }, `Sync interval unavailable: ${err.message}`)
      );
    }
  }

  intervalSel.addEventListener('change', async () => {
    intervalSel.disabled = true;
    try {
      const s = await api.post('/settings/upstream-sync', { interval: intervalSel.value });
      toast(`Auto-sync interval set to ${s.interval}`, 'ok');
      intervalSel.value = s.interval;
    } catch (err) {
      toast(`Failed to save interval: ${err.message}`, 'error');
      await loadSyncSettings();
    } finally {
      intervalSel.disabled = false;
    }
  });

  async function load(force = false) {
    if (loading) return;
    loading = true;
    try {
      const url = force ? '/upstream-quotas?force=1' : '/upstream-quotas';
      const res = await api.get(url);
      if (!alive) return;
      providersData = res.providers || [];
      loaded = true;
      render();
    } catch (err) {
      if (!alive) return;
      content.replaceChildren(
        h('div', { class: 'banner err' }, icon('alert'), ` Failed to load upstream quotas: ${err.message}`)
      );
    } finally {
      loading = false;
    }
  }

  // 30 s Refresh cooldown, shared by all provider buttons so it survives re-renders.
  const cooldownLeft = () => Math.max(0, Math.ceil((cooldownUntil - Date.now()) / 1000));
  function paintRefresh(btn) {
    const left = cooldownLeft();
    btn.disabled = left > 0;
    btn.replaceChildren(icon('refresh'), left > 0 ? ` Cooldown (${left}s)` : 'Refresh');
  }
  function startCooldown() {
    cooldownUntil = Date.now() + 30000;
    clearInterval(cooldownTimer);
    const tick = () => {
      content.querySelectorAll('.uq-refresh').forEach(paintRefresh);
      if (!alive || cooldownLeft() <= 0) { clearInterval(cooldownTimer); cooldownTimer = null; }
    };
    cooldownTimer = setInterval(tick, 1000);
    tick();
  }

  // ---- bucket rows ----
  function renderBucket(b, conn, { famIcon = false, small = false } = {}) {
    if (b.is_credit_balance) {
      const empty = b.remaining <= 0;
      return h('div', { class: `uq-credit${small ? ' sm' : ''}` },
        h('div', null,
          h('div', { class: `uq-amt${empty ? ' uq-t-err' : ''}` }, `$${(b.remaining / 100).toFixed(2)}`),
          small ? null : h('div', { class: 'uq-lab' }, 'prepaid credit balance')),
        b.warning ? h('span', { class: 'uq-warn', title: 'Depletion warning (<25%)' }, icon('alert')) : null,
        h('span', { class: `badge ${empty ? 'err' : 'ok'}` }, empty ? 'Depleted' : 'Active'));
    }
    const percent = isPercentBucket(b);
    const m = bucketMeter(b);
    const { name, period } = splitPeriod(b.name);
    const right = [];
    if (percent) {
      if (b.used_percent != null && Number.isFinite(b.used_percent)) right.push(h('span', null, `${b.used_percent.toFixed(1)}% used`));
      if (!freshPercent(b)) {
        const state = b.incomplete ? 'incomplete'
          : b.observation_state === 'stale' ? `stale · ${timeAgo(b.observed_at)}`
          : b.observation_state === 'observed' ? `old · ${timeAgo(b.observed_at)}`
          : telemetryStateLabel(b.observation_state);
        right.push(h('span', { class: 'badge sm warn' }, state));
      }
    } else if (b.total > 100) {
      right.push(h('span', null, `${formatTokensCompact(b.used)} / ${formatTokensCompact(b.total)}`));
    }
    return h('div', { class: `uq-row${m.dim ? ' dim' : ''}${m.limit ? ' limit' : ''}` },
      h('div', { class: 'uq-row-top' },
        h('span', { class: 'uq-row-name', title: b.name },
          famIcon ? iconTile((FAMILY_META[bucketFamily(b, conn)] || FAMILY_META.other).spec, '', 'sm') : null,
          b.warning ? h('span', { class: 'uq-warn', title: 'Depletion warning (<25%)' }, icon('alert')) : null,
          h('span', { class: 'uq-t' }, name),
          period ? h('span', { class: 'badge sm' }, period) : null,
          b.scope && b.scope !== 'account' ? h('span', { class: 'badge sm' }, b.scope) : null),
        h('span', { class: `uq-row-pct uq-t-${m.tone}` }, m.label)),
      h('span', { class: `qbar ${m.tone}`, role: 'img', 'aria-label': `${b.name}: ${m.label}` }, h('i', { style: { width: `${m.pct}%` } })),
      h('div', { class: 'uq-row-foot' },
        h('span', null, b.reset_at ? `resets in ${formatResetIn(b.reset_at)}` : 'no reset'),
        h('span', null, ...right)));
  }

  function resetButton(conn, provider) {
    if (!(conn.reset_credits > 0)) return null;
    const btn = h('button', {
      class: 'badge badge-btn err',
      type: 'button',
      title: `Use 1 reset now (${conn.reset_credits} available)`,
      onclick: async () => {
        const ok = await confirmDialog({
          title: 'Spend upstream quota reset?',
          body: `Spend 1 irreversible reset grant for "${conn.account_label || conn.connection_id}"? This will refill session and weekly limits on ${provider.provider_name}. (${conn.reset_credits} resets available)`,
          danger: true,
          confirmText: 'Spend reset'
        });
        if (!ok) return;
        try {
          btn.disabled = true;
          btn.textContent = 'Resetting...';
          const res = await api.post(`/upstream-quotas/${provider.provider_id}/connections/${conn.connection_id}/reset`, {
            provider: conn.provider,
            grant_id: conn.next_grant_id || ''
          });
          toast(res.message || 'Reset applied successfully', 'ok');
          await load(true);
        } catch (e) {
          toast(`Reset failed: ${e.message}`, 'error');
        } finally {
          btn.disabled = false;
        }
      }
    }, icon('refresh'), ` ${conn.reset_credits} Reset`);
    return btn;
  }

  const sharedBadge = () => h('span', { class: 'badge sm' }, icon('globe'), 'Shared upstream pool');
  const emptyNote = (c) => h('p', { class: 'uq-empty-note' },
    c.source === 'sdk' ? telemetryStateLabel(c.observation_state) : 'No active quota buckets reported.');
  const errBanner = (c) => (c.error_msg ? h('div', { class: 'banner err', role: 'alert' }, icon('alert'), c.error_msg) : null);

  // ---- Group by model: one card per family ----
  function accountGroup(g, provider, shownReset) {
    const c = g.conn;
    const showReset = c.reset_credits > 0 && !shownReset.has(c.connection_id);
    if (showReset) shownReset.add(c.connection_id);
    const hideName = isAllowanceConn(c);
    const head = hideName && !showReset ? null : h('div', { class: 'uq-acct-head' },
      hideName ? null : subProviderTile(c.provider, c.account_label, 'sm'),
      hideName ? null : h('span', { class: 'uq-nm', title: c.account_label }, c.account_label),
      c.observation_state === 'stale' ? h('span', { class: 'badge sm warn' }, 'stale') : null,
      planOf(c) && !hideName ? h('span', { class: 'badge sm' }, planOf(c)) : null,
      isSharedPool(c, provider) ? sharedBadge() : null,
      showReset ? resetButton(c, provider) : null);
    return h('div', { class: 'uq-acct' }, head,
      h('div', { class: 'uq-acct-body' }, g.buckets.map((b) => renderBucket(b, c, { small: true }))));
  }

  function familyCard({ family, groups }, provider, shownReset) {
    const meta = FAMILY_META[family];
    const all = groups.flatMap((g) => g.buckets);
    const w = worstOf(all);
    const chip = w.limit ? h('span', { class: 'badge err' }, icon('alert'), 'Limit reached')
      : w.low != null && w.low < 30 ? h('span', { class: 'badge warn' }, `lowest ${Math.round(w.low)}%`) : null;
    const th = MODEL_THEME[family] ? ` ${MODEL_THEME[family]}` : '';
    return h('article', { class: `uq-card gb-group uq-group-box${th}${cardState(w)}${family === 'allowance' ? ' is-allowance' : ''}` },
      h('div', { class: 'uq-card-head' }, iconTile(meta.spec, meta.title),
        h('div', { class: 'uq-ttl' },
          h('h3', { title: meta.title }, meta.title),
          h('div', { class: 'uq-sub' },
            `${groups.length} ${groups.length === 1 ? 'account' : 'accounts'} · ${all.length} ${family === 'credit' ? 'balance' : 'limit'}${all.length === 1 ? '' : 's'}`,
            family === 'allowance' ? h('span', { class: 'badge sm ok' }, 'admin limit') : null)),
        h('div', { class: 'uq-side' }, chip)),
      h('div', { class: 'gb-body' }, groups.map((g) => accountGroup(g, provider, shownReset))));
  }

  function attentionCard(conns) {
    const errs = conns.some((c) => c.status === 'error' || c.status === 'degraded');
    const title = errs ? 'Needs attention' : 'Waiting for data';
    return h('article', { class: `uq-card gb-group uq-group-box${errs ? ' is-err' : ''}` },
      h('div', { class: 'uq-card-head' }, iconTile({ ic: 'circle-x' }, 'X'),
        h('div', { class: 'uq-ttl' },
          h('h3', { title }, title),
          h('div', { class: 'uq-sub' }, `${conns.length} ${conns.length === 1 ? 'account' : 'accounts'}`))),
      h('div', { class: 'gb-body' }, conns.map((c) => h('div', { class: 'uq-acct' },
        h('div', { class: 'uq-acct-head' }, subProviderTile(c.provider, c.account_label, 'sm'),
          h('span', { class: 'uq-nm', title: c.account_label }, c.account_label),
          c.plan ? h('span', { class: 'badge sm' }, c.plan) : null,
          h('span', { class: `badge sm ${c.status === 'error' ? 'err' : c.status === 'degraded' ? 'warn' : ''}` },
            c.status === 'ok' ? telemetryStateLabel(c.observation_state) : c.status)),
        errBanner(c) || emptyNote(c)))));
  }

  const subProviderTitle = (id) => {
    const s = String(id || '');
    return s ? s.charAt(0).toUpperCase() + s.slice(1) : '';
  };

  function subProviderCard({ subProvider, groups }, provider, shownReset) {
    const title = subProviderTitle(subProvider);
    const all = groups.flatMap((g) => g.buckets);
    const w = worstOf(all);
    const chip = w.limit ? h('span', { class: 'badge err' }, icon('alert'), 'Limit reached')
      : w.low != null && w.low < 30 ? h('span', { class: 'badge warn' }, `lowest ${Math.round(w.low)}%`) : null;
    const allowance = groups.length > 0 && groups.every((g) => isAllowanceConn(g.conn));
    const cardIcon = allowance ? iconTile({ ic: 'shield' }, title) : subProviderTile(subProvider, title);
    const key = String(groups[0]?.conn?.provider || subProvider || '').toLowerCase();
    const th = SUB_PROVIDER_THEME[key] ? ` ${SUB_PROVIDER_THEME[key]}` : '';
    return h('article', { class: `uq-card gb-group uq-group-box${th}${cardState(w)}${allowance ? ' is-allowance' : ''}` },
      h('div', { class: 'uq-card-head' }, cardIcon,
        h('div', { class: 'uq-ttl' },
          h('h3', { title }, title),
          h('div', { class: 'uq-sub' },
            `${groups.length} ${groups.length === 1 ? 'account' : 'accounts'} · ${all.length} ${all.length === 1 ? 'limit' : 'limits'}`,
            allowance ? h('span', { class: 'badge sm ok' }, 'admin limit') : null)),
        h('div', { class: 'uq-side' }, chip)),
      h('div', { class: 'gb-body' }, groups.map((g) => accountGroup(g, provider, shownReset))));
  }

  function cardsByProvider(p) {
    const { cards, attention } = groupProviderBySubProvider(p);
    const shownReset = new Set();
    const out = cards.map((c) => subProviderCard(c, p, shownReset));
    if (attention.length) out.push(attentionCard(attention));
    return out;
  }

  function cardsByModel(p) {
    const { cards, attention } = groupProviderByFamily(p);
    const shownReset = new Set();
    const out = cards.map((c) => familyCard(c, p, shownReset));
    if (attention.length) out.push(attentionCard(attention));
    return out;
  }

  // ---- Group by account: one card per connection ----
  function connectionCard(c, p) {
    const err = c.status === 'error';
    const buckets = (c.buckets || []).filter(Boolean);
    const w = worstOf(buckets);
    const allowance = isAllowanceConn(c);
    const multi = new Set(buckets.map((b) => bucketFamily(b, c))).size > 1;
    const sorted = [...buckets].sort((a, b) => FAMILY_ORDER.indexOf(bucketFamily(a, c)) - FAMILY_ORDER.indexOf(bucketFamily(b, c)));
    const status = c.source === 'sdk' && c.status === 'ok'
      ? h('span', { class: `badge ${c.observation_state === 'observed' ? 'ok' : 'warn'}` }, telemetryStateLabel(c.observation_state))
      : c.status !== 'ok' ? h('span', { class: `badge ${err ? 'err' : 'warn'}` }, c.status) : null;
    return h('article', { class: `uq-card${err ? ' is-err' : cardState(w)}${allowance ? ' is-allowance' : ''}` },
      h('div', { class: 'uq-card-head' }, allowance ? iconTile({ ic: 'shield' }, c.account_label) : subProviderTile(c.provider, c.account_label),
        h('div', { class: 'uq-ttl' },
          h('h3', { title: c.account_label }, c.account_label || c.connection_id),
          h('div', { class: 'uq-sub' },
            h('span', null, c.provider),
            planOf(c) ? h('span', { class: 'badge sm' }, planOf(c)) : null,
            allowance ? h('span', { class: 'badge sm ok' }, icon('user'), 'Key allowance (admin limit)') : null,
            isSharedPool(c, p) ? sharedBadge() : null)),
        h('div', { class: 'uq-side' }, resetButton(c, p), status)),
      err ? errBanner(c) : null,
      sorted.length
        ? h('div', { class: 'uq-acct-body' }, sorted.map((b) => renderBucket(b, c, { famIcon: multi })))
        : (err ? null : emptyNote(c)));
  }

  // ---- Provider section ----
  function providerSection(p) {
    const isUnsupported = p.quota_type === 'disabled';
    const sdk = p.source === 'sdk';
    const refreshBtn = h('button', {
      class: 'btn btn-sm uq-refresh',
      type: 'button',
      disabled: isUnsupported || loading,
      title: sdk ? 'Read latest SDK snapshot; no inference request is sent' : 'Force refresh live quotas from upstream',
      onclick: async (e) => {
        e.stopPropagation();
        refreshBtn.disabled = true;
        await load(true);
        toast(`Refreshed quotas for ${p.provider_name}`, 'ok');
        startCooldown();
      }
    }, icon('refresh'), 'Refresh');
    if (!isUnsupported && cooldownLeft() > 0) paintRefresh(refreshBtn);
    // Clicks anywhere on the header (including the chevron) bubble to the toggle; Refresh stops them.
    refreshBtn.addEventListener('click', (e) => e.stopPropagation());

    const protocolLabel = p.quota_type === '9router' ? 'Router Protocol'
      : p.quota_type === 'nineguard' ? 'NineGuard Cascaded'
      : sdk || p.quota_type === 'sdk' ? 'SDK account telemetry' : 'Direct LLM (No Tracker)';
    const statusText = sdk && p.status === 'ok' ? telemetryStateLabel(p.observation_state) : p.status;
    const statusClass = p.status === 'error' ? 'err'
      : p.status === 'degraded' ? 'warn'
      : p.status === 'ok' ? (sdk && p.observation_state !== 'observed' ? 'warn' : 'ok') : 'muted';
    const synced = sdk
      ? h('span', { class: 'muted uq-synced' }, observationLabel(p.observed_at, p.observation_state))
      : p.last_updated ? h('span', { class: 'muted uq-synced' }, `Synced ${timeAgo(p.last_updated)}`) : null;

    const open = isProviderOpen(p, providersData.length);
    const bodyId = `uq-body-${providerKey(p).replace(/[^\w-]/g, '_')}`;
    const chevron = h('button', {
      class: 'icon-btn sm', type: 'button', 'aria-expanded': String(open), 'aria-controls': bodyId,
      'aria-label': `${open ? 'Collapse' : 'Expand'} ${p.provider_name}`,
    }, icon(open ? 'chevron-down' : 'chevron-right'));
    const summary = h('span', { class: 'muted uq-summary', hidden: open }, upstreamProviderSummary(p));
    const conns = (p.connections || []).filter(Boolean);
    const body = h('div', { id: bodyId, class: 'uq-body', hidden: !open },
      isUnsupported
        ? h('p', { class: 'uq-empty-note' }, 'This provider connects directly to an LLM without upstream limit telemetry. Switch protocol in Providers menu to enable.')
        : conns.length
        ? h('div', { class: groupBy === 'account' ? 'uq-grid gb-acct-stage' : 'uq-grid gb-stage' },
            groupBy === 'account' ? conns.map((c) => connectionCard(c, p))
            : groupBy === 'provider' ? cardsByProvider(p)
            : cardsByModel(p))
        : h('p', { class: 'uq-empty-note' }, sdk ? 'No connected accounts. Connect an account in Providers.' : 'No account connections found on upstream.')
    );
    const setOpen = (next) => {
      setProviderOpen(p, next);
      body.hidden = !next;
      summary.hidden = next;
      chevron.setAttribute('aria-expanded', String(next));
      chevron.setAttribute('aria-label', `${next ? 'Collapse' : 'Expand'} ${p.provider_name}`);
      chevron.replaceChildren(icon(next ? 'chevron-down' : 'chevron-right'));
    };

    return h('section', { class: 'uq-prov', 'aria-label': p.provider_name },
      h('div', { class: 'uq-prov-head', onclick: () => setOpen(body.hidden) },
        h('div', { class: 'uq-prov-main' },
          chevron,
          providerMark({ name: p.provider_name, quota_type: p.quota_type, family: sdk ? conns[0]?.provider : '' }),
          h('h2', null, p.provider_name),
          h('span', { class: 'badge' }, protocolLabel),
          h('span', { class: `badge ${statusClass}` }, statusText),
          synced,
          summary),
        h('div', { class: 'uq-prov-actions' }, refreshBtn)),
      p.error_msg ? h('div', { class: 'banner err uq-prov-err' }, icon('alert'), ' ', p.error_msg) : null,
      body);
  }

  function render() {
    stageObservers.forEach((o) => o.disconnect());
    stageObservers = [];

    const activeTrackers = providersData.filter((p) => p.quota_type && p.quota_type !== 'disabled');

    if (!providersData.length || !activeTrackers.length) {
      content.replaceChildren(
        emptyState(
          'inbox',
          'No upstream quota trackers configured',
          'Configure a provider with the 9router API or cascaded NineGuard protocol in Providers to track live upstream limits.',
          h('button', {
            class: 'btn btn-primary',
            type: 'button',
            style: { marginTop: '12px' },
            onclick: () => location.hash = '#/providers'
          }, icon('server'), 'Manage providers')
        )
      );
      return;
    }

    content.replaceChildren(...providersData.map(providerSection));

    content.querySelectorAll('.gb-stage, .gb-acct-stage').forEach((st) => {
      layoutBento(st);
      if (typeof ResizeObserver !== 'undefined') {
        const ro = new ResizeObserver(() => {
          if (!alive) return;
          if (st.clientWidth !== st._gbW) {
            st._gbW = st.clientWidth;
            if (st.clientWidth > 0) layoutBento(st);
          }
        });
        st._gbW = st.clientWidth;
        ro.observe(st);
        stageObservers.push(ro);
      }
    });
  }

  loadSyncSettings();
  load(false);

  return () => {
    alive = false;
    clearInterval(cooldownTimer);
    stageObservers.forEach((o) => o.disconnect());
    stageObservers = [];
  };
}
