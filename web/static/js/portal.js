// NineGuard Client Key Self-Service Portal
import { h, icon, emptyState, toast, fmtNum, fmtCompact, timeZone } from './ui.js';
import {
  getStoredPortalKey,
  setStoredPortalKey,
  clearStoredPortalKey,
  isValidKeyFormat,
  maskKey,
  buildHeaders
} from './portalhelpers.js';
import { formatCost } from './costhelpers.js';
import { quotaPercent, formatTokensCompact } from './quotahelpers.js';
import { quotaStatus, barTone, formatResetIn } from './modelquotas.js';
import { groupDailyIntoWeeks, heatmapIntensity } from './views/keyusage.js';
import { upstreamBarTone } from './views/upstream_quotas.js';

const root = document.getElementById('portal-app');

let activeKey = getStoredPortalKey();
let activeRange = '30d'; // '7d' | '30d' | 'all'
let quotaData = null;
let usageData = null;
let modelsData = null;
let loading = false;
let errorMsg = '';

async function fetchJSON(url, key) {
  const resp = await fetch(url, {
    headers: {
      ...buildHeaders(key),
      'Accept': 'application/json'
    }
  });

  if (resp.status === 401) {
    throw new Error('UNAUTHORIZED');
  }

  const text = await resp.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch { /* non-JSON body */ }

  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    if (data && data.error) {
      msg = typeof data.error === 'string' ? data.error : (data.error.message || msg);
    }
    throw new Error(msg);
  }

  return data;
}

function initTheme() {
  const cur = document.documentElement.dataset.theme || 'dark';
  document.documentElement.dataset.theme = cur;
}

function toggleTheme() {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  try {
    localStorage.setItem('nineguard_theme', next);
  } catch {}
}

async function validateAndSaveKey(rawKey) {
  const key = rawKey.trim();
  if (!isValidKeyFormat(key)) {
    throw new Error('Key must start with sk- (e.g. sk-ng-...)');
  }

  // Probe with GET /v1/quota
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
  try {
    const [qRes, uRes, mRes] = await Promise.all([
      fetchJSON('/v1/quota', activeKey).catch(err => {
        if (err.message === 'UNAUTHORIZED') throw err;
        return null;
      }),
      fetchJSON(`/v1/usage?range=${activeRange}&tz=${encodeURIComponent(tz)}`, activeKey).catch(err => {
        if (err.message === 'UNAUTHORIZED') throw err;
        return null;
      }),
      fetchJSON('/v1/models', activeKey).catch(err => {
        if (err.message === 'UNAUTHORIZED') throw err;
        return { data: [] };
      })
    ]);

    quotaData = qRes;
    usageData = uRes;
    modelsData = (mRes && Array.isArray(mRes.data)) ? mRes.data : [];
  } catch (err) {
    if (err.message === 'UNAUTHORIZED') {
      clearStoredPortalKey();
      activeKey = '';
      toast('API key was revoked or invalid. Please reconnect.', 'error');
    } else {
      errorMsg = err.message || 'Failed to load usage data';
    }
  } finally {
    loading = false;
    render();
  }
}

// ── Render Views ──

function renderLoginView() {
  let showKey = false;

  const themeBtn = h('button', {
    class: 'icon-btn auth-theme',
    type: 'button',
    title: 'Toggle theme',
    onclick: () => {
      toggleTheme();
      updateThemeBtn();
    }
  });

  function updateThemeBtn() {
    const isDark = document.documentElement.dataset.theme === 'dark';
    themeBtn.replaceChildren(icon(isDark ? 'sun' : 'moon'));
  }
  updateThemeBtn();

  const keyInput = h('input', {
    class: 'input',
    type: 'password',
    placeholder: 'sk-ng-...',
    style: { fontSize: '14px', padding: '10px 12px' },
    autofocus: true
  });

  const toggleEyeBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    style: { minWidth: '40px' },
    onclick: () => {
      showKey = !showKey;
      keyInput.type = showKey ? 'text' : 'password';
      toggleEyeBtn.replaceChildren(icon(showKey ? 'eye-off' : 'eye'));
    }
  }, icon('eye'));

  const submitBtn = h('button', {
    class: 'btn btn-primary btn-block',
    type: 'submit',
    style: { marginTop: '16px', padding: '10px', fontSize: '14px', fontWeight: 'bold' }
  }, icon('key'), ' Access Developer Portal');

  const errBox = h('div', { class: 'note err mt-sm', style: { display: 'none' } });

  const form = h('form', {
    class: 'auth-form',
    onsubmit: async (e) => {
      e.preventDefault();
      const raw = keyInput.value;
      if (!raw.trim()) return;

      submitBtn.disabled = true;
      submitBtn.replaceChildren(icon('refresh'), ' Verifying Key...');
      errBox.style.display = 'none';

      try {
        await validateAndSaveKey(raw);
        await loadPortalData();
      } catch (err) {
        errBox.textContent = err.message || 'Invalid API key or revoked access';
        errBox.style.display = 'block';
      } finally {
        submitBtn.disabled = false;
        submitBtn.replaceChildren(icon('key'), ' Access Developer Portal');
      }
    }
  },
    h('div', { style: { display: 'flex', gap: '8px', alignItems: 'center' } },
      keyInput,
      toggleEyeBtn
    ),
    errBox,
    submitBtn
  );

  const card = h('div', { class: 'auth-card' },
    h('div', { class: 'auth-brand' },
      h('img', { src: '/appicon.png', width: '34', height: '34', alt: 'NineGuard' }),
      h('span', null, 'NineGuard')
    ),
    h('div', { class: 'auth-title' },
      h('h1', null, 'Developer Portal'),
      h('p', null, 'Enter your assigned NineGuard API key to inspect your token allowances, reset schedule, and activity history.')
    ),
    form
  );

  const authPage = h('div', { class: 'auth-page' },
    themeBtn,
    h('main', { class: 'auth-main' }, card),
    h('footer', { class: 'auth-footer' },
      h('span', null, 'NineGuard'),
      h('span', null, 'Client Key Self-Service Portal')
    )
  );

  root.replaceChildren(authPage);
}

function renderDashboardView() {
  // 1. Top Navbar
  const maskedToken = maskKey(activeKey);
  const keyName = (quotaData && quotaData.key_quota && quotaData.key_quota.key_name) || (usageData && usageData.key_name) || 'Agent Key';
  const tzName = (usageData && usageData.timezone) || timeZone() || 'UTC';

  const range7d = h('button', {
    class: `btn btn-sm ${activeRange === '7d' ? 'btn-primary' : ''}`,
    type: 'button',
    onclick: () => { activeRange = '7d'; loadPortalData(); }
  }, '7 Days');
  const range30d = h('button', {
    class: `btn btn-sm ${activeRange === '30d' ? 'btn-primary' : ''}`,
    type: 'button',
    onclick: () => { activeRange = '30d'; loadPortalData(); }
  }, '30 Days');
  const rangeAll = h('button', {
    class: `btn btn-sm ${activeRange === 'all' ? 'btn-primary' : ''}`,
    type: 'button',
    onclick: () => { activeRange = 'all'; loadPortalData(); }
  }, 'All Time');

  const rangeGroup = h('div', { style: { display: 'flex', gap: '4px' } }, range7d, range30d, rangeAll);

  const refreshBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: 'Refresh Data',
    onclick: () => loadPortalData()
  }, icon('refresh'));

  const themeBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: 'Toggle Dark / Light Theme',
    onclick: () => toggleTheme()
  }, icon('sun'));

  const logoutBtn = h('button', {
    class: 'btn btn-sm btn-danger',
    type: 'button',
    title: 'Forget this API Key',
    onclick: () => {
      clearStoredPortalKey();
      activeKey = '';
      quotaData = null;
      usageData = null;
      modelsData = null;
      render();
    }
  }, icon('logout'), ' Disconnect');

  const header = h('header', {
    style: {
      display: 'flex',
      justifyContent: 'space-between',
      alignItems: 'center',
      padding: '12px 20px',
      borderBottom: '1px solid var(--border)',
      background: 'var(--panel)',
      flexWrap: 'wrap',
      gap: '12px'
    }
  },
    h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px' } },
      h('img', { src: '/appicon.png', width: '28', height: '28', alt: 'NineGuard' }),
      h('span', { class: 'strong', style: { fontSize: '15px' } }, 'NineGuard Portal'),
      h('span', { class: 'badge', style: { fontSize: '11px', background: 'var(--hover)' } },
        icon('key'), ' ', keyName, ' (', maskedToken, ')'
      ),
      h('span', { class: 'badge muted', style: { fontSize: '10.5px' } }, tzName)
    ),
    h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
      rangeGroup,
      refreshBtn,
      themeBtn,
      logoutBtn
    )
  );

  const container = h('div', {
    style: { maxWidth: '1100px', margin: '20px auto', padding: '0 16px', display: 'flex', flexDirection: 'column', gap: '16px' }
  });

  if (loading) {
    container.append(
      h('div', { class: 'skel', style: { height: '100px' } }),
      h('div', { class: 'skel', style: { height: '140px' } }),
      h('div', { class: 'skel', style: { height: '260px' } })
    );
    root.replaceChildren(header, container);
    return;
  }

  if (errorMsg) {
    container.append(emptyState('alert', 'Could Not Load Usage Data', errorMsg));
    root.replaceChildren(header, container);
    return;
  }

  // ── Section 1: Quota Allowance Card ──
  container.append(renderQuotaCard());

  // ── Section 2: Upstream Shared Capacity Pools ──
  if (quotaData && quotaData.upstream_pools && quotaData.upstream_pools.length) {
    container.append(renderUpstreamPoolsSection());
  }

  // ── Section 3: Usage KPI Overview ──
  if (usageData) {
    container.append(renderUsageKPICards());
    container.append(renderHeatmapCard());
    container.append(renderModelsBreakdownCard());
  }

  // ── Section 4: Allowed Models Catalog ──
  container.append(renderAllowedModelsCard());

  // ── Section 5: Upstream Shared Capacity Context ──
  if (usageData && usageData.upstream_usage && usageData.upstream_usage.length) {
    container.append(renderContextFooter());
  }

  root.replaceChildren(header, container);
}

function renderQuotaCard() {
  const q = quotaData;
  if (!q || (!q.key_quota && (!q.model_quotas || !q.model_quotas.length))) {
    return h('div', { class: 'card', style: { padding: '16px 20px', background: 'var(--panel)' } },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
        icon('sparkles'),
        h('b', { style: { fontSize: '14px' } }, 'Token Budget: Unlimited'),
        h('span', { class: 'badge ok', style: { fontSize: '10.5px' } }, 'No limit enforced')
      )
    );
  }

  // Model Quotas mode
  if (q.model_quotas && q.model_quotas.length) {
    const rows = q.model_quotas.map(mq => {
      const pct = quotaPercent(mq.used, mq.limit);
      const tone = barTone(quotaStatus(pct, 80, mq.warning));
      const resetStr = mq.reset_at ? `in ${formatResetIn(mq.reset_at)}` : (mq.period || '-');

      return h('tr', null,
        h('td', { style: { fontWeight: '500' } },
          mq.warning ? h('span', { title: 'Depletion warning (>=80% used)', style: { marginRight: '6px' } }, '⚠️') : null,
          mq.model_id
        ),
        h('td', { class: 'muted num', style: { fontSize: '12px' } }, `${formatTokensCompact(mq.used)} / ${formatTokensCompact(mq.limit)}`),
        h('td', null,
          h('span', { style: { display: 'inline-flex', alignItems: 'center' } },
            h('span', { class: `qbar ${tone}`, style: { width: '80px', display: 'inline-block' } },
              h('i', { style: { width: `${Math.min(100, Math.max(1, pct))}%` } })
            ),
            h('span', { class: `qpct ${tone}`, style: { marginLeft: '8px', fontSize: '11px', fontWeight: 'bold' } }, `${pct}%`)
          )
        ),
        h('td', { class: 'muted', style: { fontSize: '12px' } }, resetStr)
      );
    });

    return h('div', {
      class: 'card',
      style: {
        background: 'color-mix(in srgb, var(--ok) 5%, var(--panel))',
        border: '1.5px solid var(--ok)',
        borderRadius: '8px',
        padding: '14px 18px',
      }
    },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px', flexWrap: 'wrap', gap: '8px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
          icon('shield'),
          h('b', { style: { fontSize: '14px' } }, 'Key Allowance (Token Budget)'),
          h('span', { class: 'badge ok', style: { fontWeight: 'bold', border: '1px solid var(--ok)' } }, '👤 Key Allowance (Admin Limit)'),
          h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px' } }, 'Per-Model Quotas')
        ),
        h('span', { class: 'badge ok' }, 'active')
      ),
      h('div', { class: 'table-wrap' },
        h('table', { class: 'table', style: { width: '100%', fontSize: '12px', margin: 0 } },
          h('thead', null,
            h('tr', null,
              h('th', { style: { width: '38%' } }, 'Model / Allowance Window'),
              h('th', { class: 'num', style: { width: '22%' } }, 'Tokens Consumed / Limit'),
              h('th', { style: { width: '25%' } }, 'Remaining'),
              h('th', { style: { width: '15%' } }, 'Reset In')
            )
          ),
          h('tbody', null, ...rows)
        )
      )
    );
  }

  // Single Key Quota mode
  if (q.key_quota) {
    const kq = q.key_quota;
    const limit = kq.limit || 0;
    const used = kq.used || 0;
    const pct = quotaPercent(used, limit);
    const tone = barTone(quotaStatus(pct, 80, kq.warning));
    const resetStr = kq.reset_at ? `in ${formatResetIn(kq.reset_at)}` : (kq.period || '-');

    const row = h('tr', null,
      h('td', { style: { fontWeight: '500' } },
        kq.warning ? h('span', { title: 'Depletion warning (>=80% used)', style: { marginRight: '6px' } }, '⚠️') : null,
        `Global Token Budget (${kq.period || 'total'})`
      ),
      h('td', { class: 'muted num', style: { fontSize: '12px' } }, `${formatTokensCompact(used)} / ${formatTokensCompact(limit)}`),
      h('td', null,
        h('span', { style: { display: 'inline-flex', alignItems: 'center' } },
          h('span', { class: `qbar ${tone}`, style: { width: '80px', display: 'inline-block' } },
            h('i', { style: { width: `${Math.min(100, Math.max(1, pct))}%` } })
          ),
          h('span', { class: `qpct ${tone}`, style: { marginLeft: '8px', fontSize: '11px', fontWeight: 'bold' } }, `${pct}%`)
        )
      ),
      h('td', { class: 'muted', style: { fontSize: '12px' } }, resetStr)
    );

    return h('div', {
      class: 'card',
      style: {
        background: 'color-mix(in srgb, var(--ok) 5%, var(--panel))',
        border: '1.5px solid var(--ok)',
        borderRadius: '8px',
        padding: '14px 18px',
      }
    },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px', flexWrap: 'wrap', gap: '8px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
          icon('shield'),
          h('b', { style: { fontSize: '14px' } }, 'Key Allowance (Token Budget)'),
          h('span', { class: 'badge ok', style: { fontWeight: 'bold', border: '1px solid var(--ok)' } }, '👤 Key Allowance (Admin Limit)'),
          h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px' } }, kq.period || 'none')
        ),
        h('span', { class: 'badge ok' }, 'active')
      ),
      h('div', { class: 'table-wrap' },
        h('table', { class: 'table', style: { width: '100%', fontSize: '12px', margin: 0 } },
          h('thead', null,
            h('tr', null,
              h('th', { style: { width: '38%' } }, 'Allowance Window'),
              h('th', { class: 'num', style: { width: '22%' } }, 'Tokens Consumed / Limit'),
              h('th', { style: { width: '25%' } }, 'Remaining'),
              h('th', { style: { width: '15%' } }, 'Reset In')
            )
          ),
          h('tbody', null, row)
        )
      )
    );
  }

  return h('div');
}

function renderUpstreamPoolsSection() {
  const pools = (quotaData && quotaData.upstream_pools) || [];
  if (!pools.length) return h('div');

  const poolCards = pools.map(pool => {
    const statusTone = pool.status === 'ok' ? 'ok' : (pool.status === 'degraded' ? 'warn' : 'err');

    const bucketRows = (pool.buckets || []).map(b => {
      const rawTone = upstreamBarTone(b.remaining_percentage);
      const tone = rawTone === 'danger' ? 'err' : rawTone;
      const pct = Math.max(0, Math.min(100, b.remaining_percentage || 0));

      let usedTotalStr = `${formatTokensCompact(b.used)} / ${formatTokensCompact(b.total)}`;
      if (b.is_credit_balance) {
        usedTotalStr = `$${(b.remaining / 100).toFixed(2)}`;
      } else if (b.total <= 0) {
        usedTotalStr = '-';
      }

      const resetStr = b.reset_at ? `in ${formatResetIn(b.reset_at)}` : '-';

      return h('tr', null,
        h('td', { style: { fontWeight: '500' } },
          b.warning ? h('span', { title: 'Depletion warning (<25%)', style: { marginRight: '6px' } }, '⚠️') : null,
          b.name
        ),
        h('td', { class: 'muted num', style: { fontSize: '12px' } }, usedTotalStr),
        h('td', null,
          b.is_credit_balance
            ? h('span', { class: 'badge ok' }, 'Active')
            : h('span', { style: { display: 'inline-flex', alignItems: 'center' } },
                h('span', { class: `qbar ${tone}`, style: { width: '80px', display: 'inline-block' } },
                  h('i', { style: { width: `${pct}%` } })
                ),
                h('span', { class: `qpct ${tone}`, style: { marginLeft: '8px', fontSize: '11px', fontWeight: 'bold' } }, `${pct}%`)
              )
        ),
        h('td', { class: 'muted', style: { fontSize: '12px' } }, resetStr)
      );
    });

    return h('div', {
      style: {
        background: 'var(--panel)',
        border: '1px solid var(--border)',
        borderRadius: '8px',
        padding: '12px 16px',
        marginBottom: '10px'
      }
    },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px', flexWrap: 'wrap', gap: '8px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
          icon('server'),
          h('span', { style: { fontWeight: '700', fontSize: '13px' } }, pool.family || pool.provider),
          h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: '600', fontSize: '11px', border: '1px solid var(--border)' } }, '🌐 Shared Upstream Pool')
        ),
        h('span', { class: `badge ${statusTone}` }, pool.status || 'ok')
      ),
      bucketRows.length > 0
        ? h('div', { class: 'table-wrap' },
            h('table', { class: 'table', style: { width: '100%', fontSize: '12px', margin: 0 } },
              h('thead', null,
                h('tr', null,
                  h('th', { style: { width: '36%' } }, 'Window / Bucket'),
                  h('th', { class: 'num', style: { width: '24%' } }, 'Used / Total'),
                  h('th', { style: { width: '25%' } }, 'Remaining'),
                  h('th', { style: { width: '15%' } }, 'Reset In')
                )
              ),
              h('tbody', null, ...bucketRows)
            )
          )
        : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, 'No active quota buckets reported.')
    );
  });

  return h('div', { class: 'card', style: { padding: '16px 20px', background: 'var(--panel)' } },
    h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '12px' } },
      icon('server'),
      h('b', { style: { fontSize: '14px' } }, 'Upstream Shared Capacity'),
      h('span', { class: 'badge muted', style: { fontSize: '11px' } }, `${pools.length} pool${pools.length === 1 ? '' : 's'}`)
    ),
    h('div', { style: { display: 'flex', flexDirection: 'column', gap: '4px' } }, ...poolCards)
  );
}

function renderUsageKPICards() {
  const s = usageData;
  const reqSub = s.errors > 0 ? `${fmtNum(s.errors)} err (${s.error_rate_pct}%)` : '100% success';
  const tokSub = `↓ ${fmtCompact(s.tokens.prompt)} · ↑ ${fmtCompact(s.tokens.completion)}`;
  const costVal = s.cost ? s.cost.display_formatted : null;
  const costSub = s.cost ? `${s.cost.currency} (est. list price)` : null;

  function kpi(label, val, sub, tone = '') {
    return h('div', { class: 'card', style: { padding: '12px 16px', background: 'var(--panel)' } },
      h('div', { class: 'muted', style: { fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.04em' } }, label),
      h('div', { style: { fontSize: '18px', fontWeight: 'bold', margin: '4px 0 2px', color: tone || 'inherit' } }, val),
      sub ? h('div', { class: 'muted', style: { fontSize: '11px' } }, sub) : null
    );
  }

  const cards = [
    kpi('Requests', fmtNum(s.requests), reqSub, 'var(--lv-info)'),
    kpi('Total Tokens', fmtCompact(s.tokens.total), tokSub, 'var(--ok)'),
    kpi('Sessions (inferred)', fmtNum(s.sessions), '30m inactivity gap', 'var(--text)'),
    kpi('Cache Hit Rate', `${s.cache_hit_pct}%`, `${fmtCompact(s.tokens.cache_read)} read`, 'var(--accent)')
  ];

  if (costVal) {
    cards.push(kpi('Est. Cost', costVal, costSub, '#eab308'));
  }

  cards.push(
    kpi('Active Days', fmtNum(s.active_days), `in ${s.range} window`),
    kpi('Peak Usage Hour', s.peak_hour_formatted, `local ${s.timezone}`),
    kpi('Thinking / Fallbacks', `${s.thinking.delivered} / ${s.fallbacks}`, `${s.thinking.lost} lost`)
  );

  return h('div', { class: 'grid grid-4', style: { gap: '10px' } }, ...cards);
}

function renderHeatmapCard() {
  const s = usageData;
  const dailyPoints = s.daily || [];
  const maxDayTokens = Math.max(...dailyPoints.map(p => p.tokens || 0), 1);
  const weeks = groupDailyIntoWeeks(dailyPoints);

  const weekdays = ['Mon', '', 'Wed', '', 'Fri', '', 'Sun'];

  const weekCols = weeks.map(w => {
    const dayCells = w.days.map(d => {
      const lvl = heatmapIntensity(d.tokens, maxDayTokens);
      const bgColors = [
        'var(--border)',
        'rgba(56, 189, 248, 0.35)',
        'rgba(56, 189, 248, 0.65)',
        'rgba(56, 189, 248, 0.90)',
        '#38bdf8'
      ];
      const cur = usageData?.cost?.currency || 'USD';
      const rate = usageData?.cost?.rate || 1.0;
      const costTip = (d.cost != null && usageData?.cost) ? ` · ${formatCost(d.cost, cur, rate, 0)}` : '';
      return h('div', {
        style: {
          width: '11px',
          height: '11px',
          borderRadius: '2px',
          background: d.inRange ? bgColors[lvl] : 'transparent',
          opacity: d.inRange ? '1' : '0.2'
        },
        title: `${d.date}: ${fmtNum(d.tokens)} tokens (${fmtNum(d.requests)} requests)${costTip}`
      });
    });
    return h('div', { style: { display: 'flex', flexDirection: 'column', gap: '3px' } }, ...dayCells);
  });

  const weekdayLabels = weekdays.map(name =>
    h('div', { style: { height: '11px', fontSize: '9px', lineHeight: '11px', color: 'var(--muted)', textAlign: 'right', width: '22px' } }, name)
  );

  return h('div', { class: 'card', style: { padding: '16px 20px', background: 'var(--panel)' } },
    h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' } },
      h('b', { style: { fontSize: '13px' } }, 'Activity Heatmap (Daily Token Load)'),
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px', fontSize: '10.5px', color: 'var(--muted)' } },
        'Less',
        h('span', { style: { width: '9px', height: '9px', borderRadius: '2px', background: 'var(--border)' } }),
        h('span', { style: { width: '9px', height: '9px', borderRadius: '2px', background: 'rgba(56, 189, 248, 0.35)' } }),
        h('span', { style: { width: '9px', height: '9px', borderRadius: '2px', background: 'rgba(56, 189, 248, 0.65)' } }),
        h('span', { style: { width: '9px', height: '9px', borderRadius: '2px', background: '#38bdf8' } }),
        'More'
      )
    ),
    h('div', { style: { display: 'flex', gap: '6px', alignItems: 'center', overflowX: 'auto', paddingBottom: '4px' } },
      h('div', { style: { display: 'flex', flexDirection: 'column', gap: '3px' } }, ...weekdayLabels),
      h('div', { style: { display: 'flex', gap: '3px' } }, ...weekCols)
    )
  );
}

function renderModelsBreakdownCard() {
  const models = usageData.models || [];
  if (!models.length) return h('div');

  const cur = usageData?.cost?.currency || 'USD';
  const rate = usageData?.cost?.rate || 1.0;

  const rows = models.map(m => {
    const costStr = (m.cost != null && usageData?.cost) ? formatCost(m.cost, cur, rate, 0) : '—';
    return h('tr', null,
      h('td', { class: 'strong' }, m.model_id),
      h('td', { class: 'num' }, fmtCompact(m.prompt)),
      h('td', { class: 'num' }, fmtCompact(m.completion)),
      h('td', { class: 'num strong' }, fmtCompact(m.total)),
      h('td', { class: 'num' }, costStr),
      h('td', { class: 'num' },
        h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '6px' } },
          h('span', null, `${m.pct_tokens.toFixed(1)}%`),
          h('div', { style: { width: '50px', height: '4px', background: 'var(--border)', borderRadius: '2px', overflow: 'hidden' } },
            h('div', { style: { width: `${m.pct_tokens}%`, height: '100%', background: 'var(--accent)' } })
          )
        )
      )
    );
  });

  return h('div', { class: 'card table-card' },
    h('div', { class: 'card-head' },
      h('b', { style: { fontSize: '13px' } }, 'Model Usage Distribution')
    ),
    h('div', { class: 'table-wrap' },
      h('table', { class: 'table' },
        h('thead', null,
          h('tr', null,
            h('th', null, 'Model'),
            h('th', { class: 'num' }, 'Prompt Tokens'),
            h('th', { class: 'num' }, 'Output Tokens'),
            h('th', { class: 'num' }, 'Total Tokens'),
            h('th', { class: 'num' }, `Est. Cost (${cur})`),
            h('th', { class: 'num', style: { width: '130px' } }, 'Share %')
          )
        ),
        h('tbody', null, ...rows)
      )
    )
  );
}

function renderAllowedModelsCard() {
  const list = modelsData || [];
  if (!list.length) return h('div');

  const badges = list.map(m => {
    return h('div', {
      class: 'card',
      style: { padding: '8px 12px', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '8px', background: 'var(--panel)' }
    },
      icon('box'),
      h('span', { class: 'strong' }, m.id),
      m.owned_by ? h('span', { class: 'badge muted', style: { fontSize: '10px' } }, m.owned_by) : null
    );
  });

  return h('div', { class: 'card', style: { padding: '16px 20px', background: 'var(--panel)' } },
    h('b', { style: { fontSize: '13px', display: 'block', marginBottom: '10px' } }, `Permitted Models (${list.length})`),
    h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '8px' } }, ...badges)
  );
}

function renderContextFooter() {
  const s = usageData;
  const items = [];

  if (s.upstream_usage && s.upstream_usage.length) {
    s.upstream_usage.forEach(u => {
      if (u.summary) {
        let costPart = '';
        if (u.summary.cost && u.summary.cost.display_formatted) {
          costPart = ` · Est. cost ${u.summary.cost.display_formatted}`;
        }
        items.push(h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px', color: 'var(--muted)' } },
          icon('server'),
          h('span', null, `Upstream (${u.provider}, shared gateway capacity): ${fmtCompact(u.summary.tokens.total)} tokens${costPart}`)
        ));
      }
    });
  }

  if (!items.length) return h('div');

  return h('div', {
    class: 'card',
    style: { padding: '12px 18px', background: 'var(--hover)', display: 'flex', flexDirection: 'column', gap: '8px' }
  }, ...items);
}

function render() {
  if (!activeKey) {
    renderLoginView();
  } else {
    renderDashboardView();
  }
}

initTheme();
if (activeKey) {
  loadPortalData();
} else {
  render();
}
