// Key Usage View: Per-API-key Usage Summary with Heatmap, Sessions, and Model breakdown
import { api } from '../api.js';
import { h, icon, emptyState, toast, formDialog, fmtNum, fmtCompact, tzLabel } from '../ui.js';
import { formatCost } from '../costhelpers.js';

export function heatmapIntensity(tokens, maxTokens) {
  if (tokens <= 0 || maxTokens <= 0) return 0;
  const ratio = tokens / maxTokens;
  if (ratio <= 0.25) return 1;
  if (ratio <= 0.50) return 2;
  if (ratio <= 0.75) return 3;
  return 4;
}

export function groupDailyIntoWeeks(points = []) {
  if (!points.length) return [];
  const sorted = [...points].sort((a, b) => a.date.localeCompare(b.date));

  const firstDate = new Date(sorted[0].date + 'T00:00:00Z');
  let dayOfWeek = firstDate.getUTCDay();
  let monOffset = (dayOfWeek + 6) % 7; // Monday = 0, ..., Sunday = 6

  const pointMap = new Map(sorted.map(p => [p.date, p]));
  const lastDate = new Date(sorted[sorted.length - 1].date + 'T00:00:00Z');
  const startCal = new Date(firstDate);
  startCal.setUTCDate(startCal.getUTCDate() - monOffset);

  const weeks = [];
  let cur = new Date(startCal);

  while (cur <= lastDate || ((cur.getUTCDay() + 6) % 7 !== 0)) {
    const week = { days: [] };
    for (let i = 0; i < 7; i++) {
      const y = cur.getUTCFullYear();
      const m = String(cur.getUTCMonth() + 1).padStart(2, '0');
      const d = String(cur.getUTCDate()).padStart(2, '0');
      const ds = `${y}-${m}-${d}`;
      const pt = pointMap.get(ds);
      week.days.push({
        date: ds,
        tokens: pt ? (pt.tokens || 0) : 0,
        requests: pt ? (pt.requests || 0) : 0,
        cost: pt ? pt.cost : null,
        inRange: pointMap.has(ds)
      });
      cur.setUTCDate(cur.getUTCDate() + 1);
    }
    weeks.push(week);
  }

  return weeks;
}

export async function openKeyUsageModal(key) {
  let activeTab = 'overview'; // 'overview' | 'models'
  let activeRange = '30d'; // '7d' | '30d' | 'all'
  let summary = null;
  let loading = false;

  const wrap = h('div', { class: 'key-usage-dialog', style: { minWidth: '720px', maxWidth: '850px' } });

  // Range pills
  const pill7d = h('button', { class: 'btn btn-sm', type: 'button', onclick: () => switchRange('7d') }, '7 Days');
  const pill30d = h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: () => switchRange('30d') }, '30 Days');
  const pillAll = h('button', { class: 'btn btn-sm', type: 'button', onclick: () => switchRange('all') }, 'All Time');

  const rangeBar = h('div', { style: { display: 'flex', gap: '6px' } }, pill7d, pill30d, pillAll);

  // Tabs
  const tabOverview = h('button', { class: 'tab active', type: 'button', onclick: () => switchTab('overview') }, icon('overview'), 'Overview');
  const tabModels = h('button', { class: 'tab', type: 'button', onclick: () => switchTab('models') }, icon('box'), 'Models');
  const tabsNav = h('div', { class: 'tabs-nav', style: { marginBottom: '14px' } }, tabOverview, tabModels);

  const contentArea = h('div');

  wrap.append(
    h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' } },
      tabsNav,
      rangeBar
    ),
    contentArea
  );

  async function loadData() {
    loading = true;
    contentArea.replaceChildren(h('div', { class: 'skel', style: { height: '240px' } }));
    try {
      summary = await api.get(`/keys/${encodeURIComponent(key.id)}/usage`, {
        range: activeRange,
        tz: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
      });
      renderView();
    } catch (e) {
      contentArea.replaceChildren(emptyState('alert', 'Failed to load usage', e.message));
    } finally {
      loading = false;
    }
  }

  function switchRange(r) {
    activeRange = r;
    pill7d.className = `btn btn-sm ${r === '7d' ? 'btn-primary' : ''}`;
    pill30d.className = `btn btn-sm ${r === '30d' ? 'btn-primary' : ''}`;
    pillAll.className = `btn btn-sm ${r === 'all' ? 'btn-primary' : ''}`;
    loadData();
  }

  function switchTab(t) {
    activeTab = t;
    tabOverview.classList.toggle('active', t === 'overview');
    tabModels.classList.toggle('active', t === 'models');
    renderView();
  }

  function kpiItem(label, val, sub, tone = '') {
    return h('div', { class: 'card', style: { padding: '10px 14px', background: 'var(--panel)', border: '1px solid var(--border)' } },
      h('div', { class: 'muted', style: { fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.04em' } }, label),
      h('div', { style: { fontSize: '18px', fontWeight: 'bold', margin: '3px 0 2px', color: tone || 'inherit' } }, val),
      sub ? h('div', { class: 'muted', style: { fontSize: '11px' } }, sub) : null
    );
  }

  function renderOverview() {
    if (!summary) return;

    const s = summary;
    const reqSub = s.errors > 0 ? `${fmtNum(s.errors)} err (${s.error_rate_pct}%)` : '100% success';
    const tokSub = `↓ ${fmtCompact(s.tokens.prompt)} · ↑ ${fmtCompact(s.tokens.completion)}`;
    const costVal = s.cost ? s.cost.display_formatted : '—';
    const costSub = s.cost ? `${s.cost.currency} (est. list)` : 'Unpriced';

    const grid = h('div', { class: 'grid grid-4', style: { gap: '10px', marginBottom: '16px' } },
      kpiItem('Requests', fmtNum(s.requests), reqSub, 'var(--lv-info)'),
      kpiItem('Total Tokens', fmtCompact(s.tokens.total), tokSub, 'var(--ok)'),
      kpiItem('Est. Cost', costVal, costSub, 'var(--accent)'),
      kpiItem('Sessions (inferred)', fmtNum(s.sessions), 'Inferred via gaps', 'var(--text)'),
      kpiItem('Active Days', fmtNum(s.active_days), `in ${s.range} window`),
      kpiItem('Peak Hour', s.peak_hour_formatted, `local ${s.timezone}`),
      kpiItem('Cache Hit Rate', `${s.cache_hit_pct}%`, `${fmtCompact(s.tokens.cache_read)} read`),
      kpiItem('Thinking / Fallbacks', `${s.thinking.delivered} / ${s.fallbacks}`, `${s.thinking.lost} lost`)
    );

    // Heatmap Calendar
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
        const cur = s.cost?.currency || 'USD';
        const rate = s.cost?.rate || 1.0;
        const costTip = (d.cost != null && s.cost) ? ` · ${formatCost(d.cost, cur, rate, 0)}` : '';
        return h('div', {
          style: {
            width: '10px',
            height: '10px',
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
      h('div', { style: { height: '10px', fontSize: '9px', lineHeight: '10px', color: 'var(--muted)', textAlign: 'right', width: '22px' } }, name)
    );

    const heatmapCard = h('div', { class: 'card', style: { padding: '14px', marginBottom: '14px', background: 'var(--panel)' } },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px' } },
        h('b', { style: { fontSize: '12.5px' } }, 'Activity Heatmap (Daily Token Volume)'),
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px', fontSize: '10.5px', color: 'var(--muted)' } },
          'Less',
          h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: 'var(--border)' } }),
          h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: 'rgba(56, 189, 248, 0.35)' } }),
          h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: 'rgba(56, 189, 248, 0.65)' } }),
          h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: '#38bdf8' } }),
          'More'
        )
      ),
      h('div', { style: { display: 'flex', gap: '6px', alignItems: 'center', overflowX: 'auto', paddingBottom: '4px' } },
        h('div', { style: { display: 'flex', flexDirection: 'column', gap: '3px' } }, ...weekdayLabels),
        h('div', { style: { display: 'flex', gap: '3px' } }, ...weekCols)
      )
    );

    contentArea.replaceChildren(grid, heatmapCard);
  }

  function renderModels() {
    if (!summary) return;
    const models = summary.models || [];
    if (!models.length) {
      contentArea.replaceChildren(emptyState('box', 'No model usage recorded'));
      return;
    }

    const cur = summary?.cost?.currency || 'USD';
    const rate = summary?.cost?.rate || 1.0;

    const rows = models.map(m => {
      const costStr = (m.cost != null && summary?.cost) ? formatCost(m.cost, cur, rate, 0) : '—';
      return h('tr', null,
        h('td', { class: 'strong' }, m.model_id),
        h('td', { class: 'num' }, fmtCompact(m.prompt)),
        h('td', { class: 'num' }, fmtCompact(m.completion)),
        h('td', { class: 'num strong' }, fmtCompact(m.total)),
        h('td', { class: 'num' }, costStr),
        h('td', { class: 'num' },
          h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '6px' } },
            h('span', null, `${m.pct_tokens.toFixed(1)}%`),
            h('div', { style: { width: '45px', height: '4px', background: 'var(--border)', borderRadius: '2px', overflow: 'hidden' } },
              h('div', { style: { width: `${m.pct_tokens}%`, height: '100%', background: 'var(--accent)' } })
            )
          )
        )
      );
    });

    const table = h('div', { class: 'card table-card' },
      h('table', { class: 'table' },
        h('thead', null,
          h('tr', null,
            h('th', null, 'Model'),
            h('th', { class: 'num' }, 'Prompt Tokens'),
            h('th', { class: 'num' }, 'Completion Tokens'),
            h('th', { class: 'num' }, 'Total Tokens'),
            h('th', { class: 'num' }, `Est. Cost (${cur})`),
            h('th', { class: 'num', style: { width: '120px' } }, 'System Share')
          )
        ),
        h('tbody', null, ...rows)
      )
    );

    contentArea.replaceChildren(table);
  }

  function renderView() {
    if (activeTab === 'overview') {
      renderOverview();
    } else {
      renderModels();
    }
  }

  formDialog({
    title: `Usage Summary — ${key.name || key.key}`,
    submitText: 'Close',
    cancel: false,
    wide: true,
    extraWide: true,
    fields: [{ node: wrap }]
  });

  loadData();
}
