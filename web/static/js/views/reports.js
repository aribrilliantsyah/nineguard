// Usage Reports: In-depth breakdowns per API Key (consumers) and per Model with custom date range.
import { api } from '../api.js';
import { h, icon, emptyState, fmtNum, fmtCompact, fmtAgo, fmtDateTime, tzLabel, toast, menu } from '../ui.js';
import { setRoute } from '../state.js';
import { formatCost } from '../costhelpers.js';

export function mount(root) {
  let alive = true;
  let currentPeriod = 'today';
  let currentGroup = 'keys'; // 'keys' or 'models'
  let currentSort = 'tokens_desc'; // 'tokens_desc' | 'prompt_desc' | 'comp_desc' | 'requests_desc' | 'latency_desc' | 'name_asc'
  let queryFilter = '';
  let customStart = '';
  let customEnd = '';
  let isCustomOpen = false;
  let reportData = null;
  let costSettings = { display_currency: 'USD', effective_rate: 1.0, effective_currency: 'USD' };
  api.get('/settings/cost').then(s => { if (s) costSettings = s; }).catch(() => {});

  // Track expanded cards
  const expandedKeys = new Set();
  const expandedModels = new Set();

  const topKPI = h('div', { class: 'grid grid-4 reports-kpi' });
  const contentArea = h('div', { class: 'mt' });

  function toDateStr(d) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
  }

  // Set initial custom date bounds (e.g. 30 days ago until today)
  const now = new Date();
  const thirtyDaysAgo = new Date();
  thirtyDaysAgo.setDate(thirtyDaysAgo.getDate() - 30);
  customEnd = toDateStr(now);
  customStart = toDateStr(thirtyDaysAgo);

  // ── Custom Date Range Inputs ──
  const startInput = h('input', {
    type: 'date',
    value: customStart,
    onchange: (e) => { customStart = e.target.value; }
  });

  const endInput = h('input', {
    type: 'date',
    value: customEnd,
    onchange: (e) => { customEnd = e.target.value; }
  });

  const startWrap = h('div', { class: 'date-input-wrap' }, startInput, icon('calendar'));
  const endWrap = h('div', { class: 'date-input-wrap' }, endInput, icon('calendar'));

  const applyBtn = h('button', {
    class: 'btn btn-sm btn-primary',
    type: 'button',
    onclick: () => {
      if (!customStart || !customEnd) {
        toast('Please specify both start and end date', 'warn');
        return;
      }
      currentPeriod = 'custom';
      updatePeriodButtons();
      load();
    }
  }, 'Apply');

  const customRangeBox = h('div', {
    class: 'custom-date-box',
    style: { display: 'none' }
  },
    icon('clock'),
    h('span', { style: { fontWeight: '600' } }, 'Custom Range:'),
    h('label', { style: { display: 'inline-flex', alignItems: 'center', gap: '6px' } }, 'From:', startWrap),
    h('label', { style: { display: 'inline-flex', alignItems: 'center', gap: '6px' } }, 'To:', endWrap),
    applyBtn
  );

  // ── Period Presets (Segmented Pill) ──
  const periods = [
    { id: 'today', label: 'Today' },
    { id: '7d', label: '7 Days' },
    { id: '30d', label: '30 Days' },
    { id: 'month', label: 'This Month' },
    { id: 'last_month', label: 'Last Month' },
    { id: 'all', label: 'All Time' },
    { id: 'custom', label: 'Custom Range', iconName: 'calendar' },
  ];

  const periodSegmented = h('div', { class: 'segmented' });
  const periodBtnMap = new Map();

  periods.forEach(p => {
    const btn = h('button', {
      type: 'button',
      class: p.id === currentPeriod ? 'active' : '',
      onclick: () => {
        currentPeriod = p.id;
        if (p.id === 'custom') {
          isCustomOpen = true;
          customRangeBox.style.display = 'inline-flex';
        } else {
          isCustomOpen = false;
          customRangeBox.style.display = 'none';
          load();
        }
        updatePeriodButtons();
      }
    }, p.iconName ? icon(p.iconName) : null, p.label);
    periodBtnMap.set(p.id, btn);
    periodSegmented.append(btn);
  });

  function updatePeriodButtons() {
    periodBtnMap.forEach((btn, id) => {
      btn.classList.toggle('active', id === currentPeriod);
    });
  }

  // ── Group Switcher (Segmented Pill) ──
  const groupSegmented = h('div', { class: 'segmented' });
  const btnGroupKeys = h('button', {
    type: 'button',
    class: currentGroup === 'keys' ? 'active' : '',
    onclick: () => {
      currentGroup = 'keys';
      btnGroupKeys.classList.add('active');
      btnGroupModels.classList.remove('active');
      renderContent();
    }
  }, icon('key'), ' Client API Keys');

  const btnGroupModels = h('button', {
    type: 'button',
    class: currentGroup === 'models' ? 'active' : '',
    onclick: () => {
      currentGroup = 'models';
      btnGroupModels.classList.add('active');
      btnGroupKeys.classList.remove('active');
      renderContent();
    }
  }, icon('box'), ' Models');

  groupSegmented.append(btnGroupKeys, btnGroupModels);

  // ── Search Input ──
  const searchInput = h('div', { class: 'tool-search', style: { width: '210px' } },
    icon('search'),
    h('input', {
      type: 'text',
      placeholder: 'Filter key or model...',
      spellcheck: 'false',
      oninput: (e) => {
        queryFilter = e.target.value.trim().toLowerCase();
        renderContent();
      }
    })
  );

  // ── Sort Selector ──
  const sortSelect = h('select', {
    class: 'select',
    style: { height: '28px', fontSize: '11.5px', minWidth: '175px' },
    onchange: (e) => {
      currentSort = e.target.value;
      renderContent();
    }
  },
    h('option', { value: 'tokens_desc' }, 'Total tokens'),
    h('option', { value: 'prompt_desc' }, 'Input tokens'),
    h('option', { value: 'comp_desc' }, 'Output tokens'),
    h('option', { value: 'requests_desc' }, 'Requests'),
    h('option', { value: 'latency_desc' }, 'Latency (slowest)'),
    h('option', { value: 'name_asc' }, 'Name (A–Z)')
  );

  // ── Filter Toolbar Card ──
  const filterToolbar = h('div', { class: 'filter-card mt' },
    h('div', { class: 'filter-row' },
      h('div', { class: 'filter-group' },
        h('span', { class: 'muted', style: { fontSize: '11px', fontWeight: '600', textTransform: 'uppercase', letterSpacing: '0.04em' } }, 'Group:'),
        groupSegmented
      ),
      searchInput,
      h('div', { class: 'filter-group' },
        h('span', { class: 'muted', style: { fontSize: '11px', fontWeight: '600', textTransform: 'uppercase', letterSpacing: '0.04em' } }, 'Sort:'),
        sortSelect
      ),
      h('div', { class: 'filter-group' },
        h('span', { class: 'muted', style: { fontSize: '11px', fontWeight: '600', textTransform: 'uppercase', letterSpacing: '0.04em' } }, 'Period:'),
        periodSegmented
      )
    ),
    customRangeBox
  );

  // ── Page Header ──
  const refreshBtn = h('button', {
    class: 'icon-btn',
    type: 'button',
    title: 'Refresh Data',
    onclick: () => load()
  }, icon('refresh'));

  const exportBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    onclick: () => menu(exportBtn, [
      { label: 'Export Options' },
      { icon: 'printer', text: 'Print / PDF Summary', onClick: () => printReport() },
      { icon: 'download', text: 'Export Aggregated CSV', onClick: () => exportAggregatedCSV() },
      { icon: 'download', text: 'Export Raw Traffic Logs CSV', onClick: () => exportRawCSV() },
    ])
  }, icon('download'), 'Export');

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Usage Reports & Breakdown'),
      h('p', null, 'Token consumption per client key and per model, with cost estimates')
    ),
    h('div', { class: 'page-actions' },
      exportBtn,
      refreshBtn
    )
  );

  function renderBusinessReportPrint() {
    if (!reportData) return h('div');

    let dateRangeStr = currentPeriod.toUpperCase();
    if (reportData.start_date && reportData.end_date) {
      dateRangeStr += ` (${reportData.start_date} to ${reportData.end_date})`;
    }

    const cur = costSettings.effective_currency || 'USD';
    const rate = costSettings.effective_rate || 1.0;

    // 1. Header
    const brandRow = h('div', { class: 'brand-row' },
      h('img', { src: '/appicon.png', width: '28', height: '28', alt: 'NineGuard' }),
      h('span', { class: 'brand-title' }, 'NineGuard')
    );

    const printHead = h('div', { class: 'print-report-header' },
      h('div', null,
        brandRow,
        h('h1', { class: 'report-title' }, 'LLM Usage & Financial Summary Report'),
        h('p', { class: 'report-period' }, `Reporting Window: ${dateRangeStr} · Timezone: ${tzLabel()}`)
      ),
      h('div', { class: 'header-right' },
        h('div', null, 'Classification: ', h('b', null, 'Internal Financial')),
        h('div', null, 'Exported: ', h('b', null, `${fmtDateTime(Date.now())} (${tzLabel()})`)),
        h('div', null, 'Display Currency: ', h('b', null, cur))
      )
    );

    const freshTokens = Math.max(0, (reportData.prompt_tokens || 0) - (reportData.cache_read_tokens || 0) - (reportData.cache_write_tokens || 0));
    const cacheReadTokens = reportData.cache_read_tokens || 0;

    // 2. Executive KPI Grid (4 cards)
    const kpiGrid = h('div', { class: 'print-kpi-grid' },
      h('div', { class: 'print-kpi-card' },
        h('div', { class: 'label' }, 'Total Est. Spend'),
        h('div', { class: 'value highlight' }, formatCost(reportData.estimated_cost_usd, cur, rate, reportData.unpriced_requests)),
        h('div', { class: 'sub' }, 'API list-price equivalent')
      ),
      h('div', { class: 'print-kpi-card' },
        h('div', { class: 'label' }, 'Total Tokens'),
        h('div', { class: 'value' }, fmtCompact(reportData.total_tokens || 0)),
        h('div', { class: 'sub' }, `↓ ${fmtCompact(freshTokens)} fresh · ${fmtCompact(cacheReadTokens)} cache · ↑ ${fmtCompact(reportData.completion_tokens || 0)} out`)
      ),
      h('div', { class: 'print-kpi-card' },
        h('div', { class: 'label' }, 'Total Requests'),
        h('div', { class: 'value' }, fmtNum(reportData.total_requests || 0)),
        h('div', { class: 'sub' }, `Top Consumer: ${reportData.top_consumer_key || 'None'}`)
      ),
      h('div', { class: 'print-kpi-card' },
        h('div', { class: 'label' }, 'Workload Mix'),
        h('div', { class: 'value' }, `${reportData.keys_breakdown?.length || 0} Keys · ${reportData.models_breakdown?.length || 0} Models`),
        h('div', { class: 'sub' }, `Top Model: ${reportData.top_model || 'None'}`)
      )
    );

    // 3. Client Key Attribution Table
    const keyRows = (reportData.keys_breakdown || []).map(k => {
      const shareStr = typeof k.token_share === 'number' ? `${k.token_share.toFixed(1)}%` : '-';
      const costStr = formatCost(k.estimated_cost_usd, cur, rate, k.unpriced_requests);
      const freshK = Math.max(0, (k.prompt_tokens || 0) - (k.cache_read_tokens || 0) - (k.cache_write_tokens || 0));
      return h('tr', null,
        h('td', { class: 'strong wrap' }, k.key_name),
        h('td', { class: 'muted nowrap' }, k.key || '-'),
        h('td', { class: 'num' }, fmtNum(k.total_requests)),
        h('td', { class: 'num' }, fmtNum(freshK)),
        h('td', { class: 'num' }, fmtNum(k.cache_read_tokens || 0)),
        h('td', { class: 'num' }, fmtNum(k.completion_tokens)),
        h('td', { class: 'num strong' }, fmtNum(k.total_tokens)),
        h('td', { class: 'num strong' }, costStr),
        h('td', { class: 'num' }, shareStr)
      );
    });

    const keySection = h('div', { class: 'print-section' },
      h('h2', { class: 'print-section-title' }, 'Client API key attribution'),
      h('table', { class: 'print-table' },
        h('thead', null,
          h('tr', null,
            h('th', null, 'Client Key'),
            h('th', null, 'Masked Key'),
            h('th', { class: 'num' }, 'Requests'),
            h('th', { class: 'num' }, 'Fresh Input'),
            h('th', { class: 'num' }, 'Cache Read'),
            h('th', { class: 'num' }, 'Output Tokens'),
            h('th', { class: 'num' }, 'Total Tokens'),
            h('th', { class: 'num' }, 'Est. Spend'),
            h('th', { class: 'num' }, 'Share %')
          )
        ),
        h('tbody', null, ...keyRows)
      ),
      h('p', { class: 'print-note' }, "Cache reads are prompt tokens served from the provider's cache and are billed at a reduced rate.")
    );

    // 4. Model Utilization Table
    const modelRows = (reportData.models_breakdown || []).map(m => {
      const shareStr = typeof m.token_share === 'number' ? `${m.token_share.toFixed(1)}%` : '-';
      const costStr = formatCost(m.estimated_cost_usd, cur, rate, m.unpriced_requests);
      const freshM = Math.max(0, (m.prompt_tokens || 0) - (m.cache_read_tokens || 0) - (m.cache_write_tokens || 0));
      return h('tr', null,
        h('td', { class: 'strong' }, m.model),
        h('td', { class: 'num' }, fmtNum(m.total_requests)),
        h('td', { class: 'num' }, fmtNum(freshM)),
        h('td', { class: 'num' }, fmtNum(m.cache_read_tokens || 0)),
        h('td', { class: 'num' }, fmtNum(m.completion_tokens)),
        h('td', { class: 'num strong' }, fmtNum(m.total_tokens)),
        h('td', { class: 'num strong' }, costStr),
        h('td', { class: 'num' }, shareStr)
      );
    });

    const modelSection = h('div', { class: 'print-section' },
      h('h2', { class: 'print-section-title' }, 'Model Utilization & Cost Breakdown'),
      h('table', { class: 'print-table' },
        h('thead', null,
          h('tr', null,
            h('th', null, 'Target Model'),
            h('th', { class: 'num' }, 'Requests'),
            h('th', { class: 'num' }, 'Fresh Input'),
            h('th', { class: 'num' }, 'Cache Read'),
            h('th', { class: 'num' }, 'Output Tokens'),
            h('th', { class: 'num' }, 'Total Tokens'),
            h('th', { class: 'num' }, 'Est. Spend'),
            h('th', { class: 'num' }, 'Share %')
          )
        ),
        h('tbody', null, ...modelRows)
      ),
      h('p', { class: 'print-note' }, "Cache reads are prompt tokens served from the provider's cache and are billed at a reduced rate.")
    );

    // 5. Footer
    const footer = h('div', { class: 'print-footer' },
      h('span', null, 'Generated by NineGuard Universal Gateway & LLM Firewall'),
      h('span', null, `Reporting Period: ${dateRangeStr}`)
    );

    return h('div', { id: 'business-report-print', class: 'print-doc' },
      printHead,
      kpiGrid,
      keySection,
      modelSection,
      footer
    );
  }

  // Injects the report as the only printed node, names the tab so "Save as PDF" suggests a
  // useful filename, and undoes both once the print dialog closes.
  function printReport() {
    if (!reportData) return;
    document.querySelectorAll('.print-doc').forEach(n => n.remove());
    const printDoc = renderBusinessReportPrint();
    document.body.append(printDoc);

    const period = currentPeriod === 'custom' ? `${customStart}_to_${customEnd}` : currentPeriod;
    const prevTitle = document.title;
    document.title = `NineGuard-usage-report-${period}-${toDateStr(new Date())}`;
    let restored = false;
    const restore = () => {
      if (restored) return;
      restored = true;
      window.removeEventListener('afterprint', restore);
      printDoc.remove();
      document.title = prevTitle;
    };
    window.addEventListener('afterprint', restore);

    const images = [...printDoc.querySelectorAll('img')].map(img => img.decode().catch(() => {}));
    Promise.all(images).then(() => window.print());
  }

  async function exportAggregatedCSV() {
    try {
      toast('Preparing Aggregated Summary CSV export...');
      await api.download('/traffic/report/export', {
        period: currentPeriod,
        start: currentPeriod === 'custom' ? customStart : '',
        end: currentPeriod === 'custom' ? customEnd : ''
      });
    } catch (err) {
      toast(`Export failed: ${err.message}`, 'error');
    }
  }

  async function exportRawCSV() {
    try {
      toast('Preparing Raw Traffic Logs CSV export...');
      await api.download('/traffic/export', {
        period: currentPeriod,
        start: currentPeriod === 'custom' ? customStart : '',
        end: currentPeriod === 'custom' ? customEnd : '',
        format: 'csv'
      });
    } catch (err) {
      toast(`Export failed: ${err.message}`, 'error');
    }
  }

  // ── Last Active (all-time, not limited to the selected period) ──
  function renderLastActive(ts) {
    const ms = ts ? Date.parse(ts) : NaN;
    if (isNaN(ms)) return h('span', { class: 'muted', style: { fontSize: '11px' } }, 'Last active: Never');
    return h('span', {
      class: 'muted',
      style: { fontSize: '11px' },
      title: `${fmtDateTime(ms)} (${tzLabel()}) · all-time, any status`,
    }, `Last active: ${fmtAgo(ms)}`);
  }

  // ── Visual Token Ratio Bar Helper ──
  function renderRatioBar(promptTokens, compTokens, title = '') {
    const prompt = promptTokens || 0;
    const comp = compTokens || 0;
    const total = prompt + comp;
    if (total <= 0) return null;

    const promptPct = ((prompt / total) * 100);
    const compPct = ((comp / total) * 100);
    const minPrompt = prompt > 0 ? Math.max(2, promptPct) : 0;
    const minComp = comp > 0 ? Math.max(2, compPct) : 0;

    const tooltip = title || `Input (Prompt): ${fmtNum(prompt)} (${promptPct.toFixed(1)}%) | Output (Response): ${fmtNum(comp)} (${compPct.toFixed(1)}%)`;

    return h('div', { class: 'token-ratio-bar', title: tooltip },
      prompt > 0 ? h('div', { class: 'token-ratio-prompt', style: { width: `${minPrompt}%` } }) : null,
      comp > 0 ? h('div', { class: 'token-ratio-comp', style: { width: `${minComp}%` } }) : null
    );
  }

  // ── KPI Helper ──
  function kpiCard(title, mainVal, subVal, ic = 'activity', badgeText = null, isWarn = false) {
    return h('div', { class: `card stat-tile${isWarn ? ' kpi-warn' : ''}` },
      h('div', { class: 'stat-label', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } }, icon(ic), h('span', null, title)),
        badgeText ? h('span', { class: `badge ${isWarn ? 'warn' : 'ok'}`, style: { fontSize: '10px' } }, badgeText) : null
      ),
      h('div', { class: 'stat-main' },
        h('div', { class: 'stat-value', style: { fontSize: '20px' } }, mainVal)
      ),
      h('div', { class: 'stat-sub', style: { fontSize: '12px', marginTop: '4px' } }, subVal)
    );
  }

  function fmtDur(ms) {
    if (!ms || ms <= 0) return '-';
    if (ms < 1000) return `${Math.round(ms)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  }

  // ── Render Top KPIs ──
  function renderKPIs() {
    if (!reportData) return;

    const topKey = reportData.top_consumer_key || 'None';
    const topMod = reportData.top_model || 'None';
    const totToks = reportData.total_tokens || 0;
    const promptToks = reportData.prompt_tokens || 0;
    const compToks = reportData.completion_tokens || 0;
    const cacheReadToks = reportData.cache_read_tokens || 0;
    const cacheWriteToks = reportData.cache_write_tokens || 0;
    const freshToks = Math.max(0, promptToks - cacheReadToks - cacheWriteToks);

    const totalReq = reportData.total_requests || 0;
    let totalSuccess = 0;
    let totalBlocked = 0;
    let totalErrors = 0;
    let latWeighted = 0;
    let latReqs = 0;
    (reportData.keys_breakdown || []).forEach(k => {
      const reqs = k.total_requests || 0;
      totalSuccess += (k.success_requests || 0);
      totalBlocked += (k.blocked_requests || 0);
      totalErrors += (k.error_requests || 0);
      if (k.avg_duration_ms > 0 && reqs > 0) {
        latWeighted += k.avg_duration_ms * reqs;
        latReqs += reqs;
      }
    });

    // Error rate: errored requests over all requests in the period.
    const errRate = totalReq > 0 ? (totalErrors / totalReq) * 100 : null;
    const errWarn = errRate !== null && errRate > 5;
    const errVal = errRate === null ? '—' : `${errRate < 10 ? errRate.toFixed(1) : errRate.toFixed(0)}%`;
    const errSub = `${fmtNum(totalErrors)} errors · ${fmtNum(totalBlocked)} blocked`;

    // Avg latency: request-weighted mean of per-key averages.
    const avgLat = latReqs > 0 ? latWeighted / latReqs : 0;
    const latVal = avgLat > 0 ? fmtDur(avgLat) : '—';
    const latSub = latReqs > 0 ? `Weighted over ${fmtNum(latReqs)} requests` : 'No timed requests';

    // Tokens saved: total plus the plugin that saved the most.
    const saved = reportData.tokens_saved || 0;
    const topSaver = (reportData.plugins_breakdown || [])
      .filter(p => (p.tokens_saved || 0) > 0)
      .sort((a, b) => (b.tokens_saved || 0) - (a.tokens_saved || 0))[0];
    const savedSub = topSaver
      ? h('span', { title: `${fmtNum(topSaver.tokens_saved)} tokens saved by ${topSaver.plugin_id}` }, `Top: ${topSaver.plugin_id} · ${fmtCompact(topSaver.tokens_saved)}`)
      : 'No plugin savings';

    // Sub-content for Total Tokens Card
    const tokensSub = h('div', null,
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px', fontSize: '11px', marginBottom: '5px' } },
        h('span', { class: 'tok-in', title: `Fresh Input: ${fmtNum(freshToks)} tokens` }, `↓ ${fmtCompact(freshToks)} fresh`),
        h('span', { class: 'muted' }, '·'),
        h('span', { class: 'muted', title: `Cache Read: ${fmtNum(cacheReadToks)} tokens` }, `${fmtCompact(cacheReadToks)} cache`),
        h('span', { class: 'muted' }, '·'),
        h('span', { class: 'tok-out', title: `Output Response: ${fmtNum(compToks)} tokens` }, `↑ ${fmtCompact(compToks)} out`)
      ),
      totToks > 0 ? renderRatioBar(promptToks, compToks) : null
    );

    // Sub-content for Total Requests Card
    const requestsSub = h('div', { style: { fontSize: '11px', display: 'flex', flexDirection: 'column', gap: '2px' } },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
        h('span', { style: { color: 'var(--ok)', fontWeight: '600' } }, `✓ ${fmtNum(totalSuccess)} ok`),
        totalBlocked > 0 ? h('span', { style: { color: 'var(--danger)', fontWeight: '600' } }, `✕ ${fmtNum(totalBlocked)} blk`) : null
      ),
      h('div', { class: 'muted', style: { fontSize: '10.5px' } }, `${reportData.keys_breakdown?.length || 0} active keys · ${reportData.models_breakdown?.length || 0} models`)
    );

    // Order: volume & cost (row 1), then quality and top consumer/model (row 2).
    topKPI.replaceChildren(
      kpiCard('Total requests', fmtNum(totalReq), requestsSub, 'activity'),
      kpiCard('Total tokens', h('span', { title: `${fmtNum(totToks)} total tokens` }, fmtCompact(totToks)), tokensSub, 'sparkles'),
      kpiCard('Estimated cost', formatCost(reportData.estimated_cost_usd, costSettings.effective_currency, costSettings.effective_rate, reportData.unpriced_requests), 'API list-price equivalent', 'dollar-sign'),
      kpiCard('Tokens saved', h('span', { title: `${fmtNum(saved)} tokens saved` }, fmtCompact(saved)), savedSub, 'scissors'),
      kpiCard('Error rate', errVal, errSub, 'circle-x', errWarn ? 'Above 5%' : null, errWarn),
      kpiCard('Avg latency', latVal, latSub, 'timer'),
      kpiCard('Top token consumer', topKey, 'Key consuming the most tokens', 'key', 'Top key'),
      kpiCard('Top model', topMod, 'Model handling the highest token load', 'box', 'Top model')
    );
  }

  // ── Sort Helper ──
  function sortItems(list, group) {
    return [...list].sort((a, b) => {
      switch (currentSort) {
        case 'tokens_desc':
          return (b.total_tokens || 0) - (a.total_tokens || 0);
        case 'prompt_desc':
          return (b.prompt_tokens || 0) - (a.prompt_tokens || 0);
        case 'comp_desc':
          return (b.completion_tokens || 0) - (a.completion_tokens || 0);
        case 'requests_desc':
          return (b.total_requests || 0) - (a.total_requests || 0);
        case 'latency_desc':
          return (b.avg_duration_ms || 0) - (a.avg_duration_ms || 0);
        case 'name_asc': {
          const nameA = group === 'keys' ? (a.key_name || a.key || '') : (a.model || '');
          const nameB = group === 'keys' ? (b.key_name || b.key || '') : (b.model || '');
          return nameA.localeCompare(nameB);
        }
        default:
          return (b.total_tokens || 0) - (a.total_tokens || 0);
      }
    });
  }

  // ── Legend & Status Bar Helper ──
  function renderLegend(totalItems, filteredCount) {
    let sortLabel = 'Total tokens';
    if (currentSort === 'prompt_desc') sortLabel = 'Input tokens';
    else if (currentSort === 'comp_desc') sortLabel = 'Output tokens';
    else if (currentSort === 'requests_desc') sortLabel = 'Requests';
    else if (currentSort === 'latency_desc') sortLabel = 'Latency (slowest)';
    else if (currentSort === 'name_asc') sortLabel = 'Name (A–Z)';

    const countText = filteredCount < totalItems
      ? `${filteredCount} of ${totalItems} ${currentGroup === 'keys' ? 'Keys' : 'Models'}`
      : `${totalItems} ${currentGroup === 'keys' ? 'API Keys' : 'Models'}`;

    return h('div', { class: 'report-legend mt' },
      h('div', { class: 'legend-item' },
        h('span', { class: 'tok-dot-in' }),
        h('span', { class: 'strong tok-in' }, 'Input (Prompt):'),
        h('span', { class: 'muted' }, 'Prompt and instructions sent to the model')
      ),
      h('div', { class: 'legend-item' },
        h('span', { class: 'tok-dot-out' }),
        h('span', { class: 'strong tok-out' }, 'Output (Response):'),
        h('span', { class: 'muted' }, 'Text generated by the model')
      ),
      h('div', { class: 'legend-item', style: { marginLeft: 'auto', gap: '8px' } },
        h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px' } }, countText),
        h('span', { class: 'muted', style: { fontSize: '11px' } }, 'Sort: ', h('b', { style: { color: 'var(--text)' } }, sortLabel))
      )
    );
  }

  // ── Table-like Header Row for the Cards ──
  function renderListHeader(group) {
    return h('div', { class: 'report-list-header mt-sm' },
      h('div', { class: 'col-entity' }, group === 'keys' ? 'Client API Key' : 'AI Model'),
      h('div', { class: 'breakdown-metrics' },
        h('div', { class: 'metric-col-tokens', style: { textAlign: 'right' } },
          h('span', null, 'Total Tokens'),
          h('div', { class: 'sub-hdr' }, '↓ Fresh · Cache · ↑ Out')
        ),
        h('div', { class: 'metric-col-cost', style: { textAlign: 'right', minWidth: '85px' } },
          h('span', null, 'Est. Cost')
        ),
        h('div', { class: 'metric-col-requests', style: { textAlign: 'right' } },
          h('span', null, 'Requests'),
          h('div', { class: 'sub-hdr' }, '✓ OK · ✕ Blk')
        ),
        h('div', { class: 'metric-col-latency', style: { textAlign: 'right' } },
          h('span', null, 'Avg Latency')
        ),
        h('div', { class: 'metric-col-share', style: { textAlign: 'left' } },
          h('span', null, 'System Share')
        )
      ),
      h('div', { style: { width: '85px', textAlign: 'right' } }, 'Action')
    );
  }

  // ── Render Group By Client API Key ──
  function renderKeysBreakdown() {
    const rawList = reportData.keys_breakdown || [];
    let list = rawList;
    if (queryFilter) {
      list = list.filter(k => (k.key_name || '').toLowerCase().includes(queryFilter) || (k.key || '').toLowerCase().includes(queryFilter));
    }

    if (!list.length) {
      return emptyState('inbox', 'No API key activity found', 'Requests sent through NineGuard with client API keys will show full breakdown here.');
    }

    const sortedList = sortItems(list, 'keys');
    const legendEl = renderLegend(rawList.length, sortedList.length);
    const headerEl = renderListHeader('keys');

    const cards = sortedList.map((k) => {
      const cardId = k.key_id ? `id:${k.key_id}` : `name:${k.key_name}`;
      const isExpanded = expandedKeys.has(cardId);

      const chevron = icon(isExpanded ? 'chevron-down' : 'chevron-right');
      const shareVal = typeof k.token_share === 'number' ? `${k.token_share.toFixed(1)}%` : '0%';
      const freshK = Math.max(0, (k.prompt_tokens || 0) - (k.cache_read_tokens || 0) - (k.cache_write_tokens || 0));

      const itemCard = h('div', { class: 'breakdown-card', style: { marginBottom: '10px', overflow: 'hidden' } },
        h('div', {
          class: 'breakdown-header',
          style: {
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '12px 18px',
            cursor: 'pointer',
            background: isExpanded ? 'var(--hover)' : 'var(--panel)',
            borderBottom: isExpanded ? '1px solid var(--border)' : 'none',
            gap: '12px'
          },
          onclick: () => {
            if (isExpanded) expandedKeys.delete(cardId);
            else expandedKeys.add(cardId);
            renderContent();
          }
        },
          // Left: Key Identity
          h('div', { class: 'col-entity', style: { display: 'flex', alignItems: 'center', gap: '10px' } },
            chevron,
            h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: 'bold', fontSize: '12.5px', padding: '4px 8px' } },
              icon('key'), ' ', k.key_name
            ),
            k.unlinked ? h('span', { class: 'badge muted', style: { fontSize: '10.5px' }, title: 'Traffic from a deleted key, or recorded before keys were linked by ID. Grouped by the key name at the time of the request.' }, 'unlinked') : null,
            h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)
          ),

          // Middle: Metrics Columns
          h('div', { class: 'breakdown-metrics', style: { fontSize: '12px' } },
            // Tokens Column
            h('div', { class: 'metric-col-tokens', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(k.total_tokens)),
              h('div', { style: { fontSize: '11px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '3px' } },
                h('span', { class: 'tok-in', title: `Fresh Input: ${fmtNum(freshK)} tokens` }, `↓ ${fmtCompact(freshK)}`),
                h('span', { class: 'muted' }, '·'),
                h('span', { class: 'muted', title: `Cache Read: ${fmtNum(k.cache_read_tokens || 0)} tokens` }, `${fmtCompact(k.cache_read_tokens || 0)}c`),
                h('span', { class: 'muted' }, '/'),
                h('span', { class: 'tok-out', title: `Output Response: ${fmtNum(k.completion_tokens)} tokens` }, `↑ ${fmtCompact(k.completion_tokens)}`)
              ),
              renderRatioBar(k.prompt_tokens, k.completion_tokens)
            ),
            // Cost Column
            h('div', { class: 'metric-col-cost', style: { textAlign: 'right', minWidth: '85px' } },
              h('div', { style: { fontSize: '13px', fontWeight: 'bold' } }, formatCost(k.estimated_cost_usd, costSettings.effective_currency, costSettings.effective_rate, k.unpriced_requests)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'est. list')
            ),
            // Requests Column
            h('div', { class: 'metric-col-requests', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } },
                fmtNum(k.total_requests),
                h('small', { class: 'muted', style: { fontSize: '11px', fontWeight: 'normal', marginLeft: '3px' } }, 'reqs')
              ),
              h('div', { style: { fontSize: '11px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '4px' } },
                h('span', { style: { color: 'var(--ok)', fontWeight: '600' }, title: `${fmtNum(k.success_requests)} successful requests` }, `✓ ${k.success_requests}`),
                k.blocked_requests > 0
                  ? h('span', { style: { color: 'var(--danger)', fontWeight: '600' }, title: `${fmtNum(k.blocked_requests)} blocked requests` }, `✕ ${k.blocked_requests}`)
                  : null
              )
            ),
            // Latency Column
            h('div', { class: 'metric-col-latency', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '13px', fontWeight: '600' } }, fmtDur(k.avg_duration_ms)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'avg lat')
            ),
            // Share of System Column
            h('div', { class: 'metric-col-share' },
              h('div', { style: { display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '3px' } },
                h('span', { class: 'muted' }, 'Share'),
                h('span', { style: { fontWeight: '600' } }, shareVal)
              ),
              h('div', { style: { width: '100%', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                h('div', { style: { width: `${Math.min(100, Math.max(3, k.token_share || 0))}%`, height: '100%', background: 'var(--accent)', borderRadius: '3px' } })
              )
            )
          ),

          // Right: Action button
          h('div', { style: { width: '85px', display: 'flex', justifyContent: 'flex-end', alignItems: 'center' } },
            h('button', {
              class: 'btn btn-sm',
              type: 'button',
              onclick: (e) => {
                e.stopPropagation();
                setRoute('traffic', { key: k.key_name || k.key });
              }
            }, icon('activity'), 'View Logs')
          )
        )
      );

      // Expanded Sub-table: Models used by this Key
      if (isExpanded) {
        const models = [...(k.model_usage || [])].sort((a, b) => (b.total_tokens || 0) - (a.total_tokens || 0));
        const modelRows = models.map((m) => {
          const modShare = typeof m.share === 'number' ? `${m.share.toFixed(1)}%` : '-';
          const costStr = formatCost(m.estimated_cost_usd, costSettings.effective_currency, costSettings.effective_rate, m.unpriced_requests);
          const freshM = Math.max(0, (m.prompt_tokens || 0) - (m.cache_read_tokens || 0) - (m.cache_write_tokens || 0));
          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'swatch', style: { display: 'inline-block', width: '8px', height: '8px', borderRadius: '2px', background: 'var(--lv-info)', marginRight: '6px' } }),
              m.model
            ),
            h('td', { class: 'num' }, fmtNum(m.requests)),
            h('td', { class: 'num tok-in' }, fmtNum(freshM)),
            h('td', { class: 'num muted' }, fmtNum(m.cache_read_tokens || 0)),
            h('td', { class: 'num tok-out' }, fmtNum(m.comp_tokens)),
            h('td', null, renderRatioBar(m.prompt_tokens, m.comp_tokens)),
            h('td', { class: 'num strong' }, fmtNum(m.total_tokens)),
            h('td', { class: 'num strong', style: { color: 'var(--text)' } }, costStr),
            h('td', { style: { minWidth: '100px' } },
              h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
                h('div', { style: { flex: '1', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                  h('div', { style: { width: `${Math.min(100, Math.max(2, m.share || 0))}%`, height: '100%', background: 'var(--lv-info)' } })
                ),
                h('span', { style: { fontSize: '11px', minWidth: '35px', textAlign: 'right' } }, modShare)
              )
            )
          );
        });

        const detailSection = h('div', { style: { padding: '16px 20px', background: 'var(--bg)' } },
          h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
            h('b', { style: { fontSize: '13px' } }, `Models Used by "${k.key_name}"`),
            renderLastActive(k.last_active_at)
          ),
          h('div', { class: 'table-wrap', style: { background: 'var(--panel)', borderRadius: '6px', border: '1px solid var(--border)' } },
            h('table', { class: 'table' },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Model'),
                  h('th', { class: 'num' }, 'Requests'),
                  h('th', { class: 'num', style: { minWidth: '100px' } },
                    h('span', { class: 'tok-in' }, 'Fresh Input')
                  ),
                  h('th', { class: 'num', style: { minWidth: '95px' } },
                    h('span', { class: 'muted' }, 'Cache Read')
                  ),
                  h('th', { class: 'num', style: { minWidth: '100px' } },
                    h('span', { class: 'tok-out' }, 'Output')
                  ),
                  h('th', { style: { width: '90px' } }, 'In / Out Ratio'),
                  h('th', { class: 'num' }, 'Total Tokens'),
                  h('th', { class: 'num', style: { minWidth: '95px' } }, 'Est. Cost'),
                  h('th', { style: { minWidth: '100px' } }, 'Key Share')
                )
              ),
              h('tbody', null,
                modelRows.length ? modelRows : h('tr', null, h('td', { colspan: 9, class: 'muted' }, 'No model breakdown available.'))
              )
            )
          )
        );

        itemCard.append(detailSection);
      }

      return itemCard;
    });

    // Linked keys first; traffic from deleted / unlinked keys in its own section.
    const linkedCards = cards.filter((_, i) => !sortedList[i].unlinked);
    const unlinkedCards = cards.filter((_, i) => sortedList[i].unlinked);
    return h('div', null, legendEl, headerEl, ...linkedCards,
      unlinkedCards.length ? h('h3', { class: 'muted', style: { fontSize: '12px', margin: '18px 0 8px' } }, 'Unlinked / deleted keys') : null,
      ...unlinkedCards);
  }

  // ── Render Group By Model ──
  function renderModelsBreakdown() {
    const rawList = reportData.models_breakdown || [];
    let list = rawList;
    if (queryFilter) {
      list = list.filter(m => (m.model || '').toLowerCase().includes(queryFilter));
    }

    if (!list.length) {
      return emptyState('inbox', 'No model activity found', 'Requests routed to AI models will display token consumers and shares here.');
    }

    const sortedList = sortItems(list, 'models');
    const legendEl = renderLegend(rawList.length, sortedList.length);
    const headerEl = renderListHeader('models');

    const cards = sortedList.map((m) => {
      const isExpanded = expandedModels.has(m.model);
      const chevron = icon(isExpanded ? 'chevron-down' : 'chevron-right');
      const shareVal = typeof m.token_share === 'number' ? `${m.token_share.toFixed(1)}%` : '0%';
      const freshM = Math.max(0, (m.prompt_tokens || 0) - (m.cache_read_tokens || 0) - (m.cache_write_tokens || 0));

      const itemCard = h('div', { class: 'breakdown-card', style: { marginBottom: '10px', overflow: 'hidden' } },
        h('div', {
          class: 'breakdown-header',
          style: {
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '12px 18px',
            cursor: 'pointer',
            background: isExpanded ? 'var(--hover)' : 'var(--panel)',
            borderBottom: isExpanded ? '1px solid var(--border)' : 'none',
            gap: '12px'
          },
          onclick: () => {
            if (isExpanded) expandedModels.delete(m.model);
            else expandedModels.add(m.model);
            renderContent();
          }
        },
          // Left: Model Identity
          h('div', { class: 'col-entity', style: { display: 'flex', alignItems: 'center', gap: '10px' } },
            chevron,
            h('span', { style: { fontWeight: 'bold', fontSize: '13.5px' } }, m.model),
            m.removed ? h('span', { class: 'badge removed removed-tag', title: 'This model was removed from its provider. Usage history is kept.' }, 'removed') : null,
            m.enabled
              ? h('span', { class: 'badge ok', style: { fontSize: '10px' } }, 'Active')
              : h('span', { class: 'badge err', style: { fontSize: '10px' } }, 'Disabled')
          ),

          // Middle: Metrics Columns
          h('div', { class: 'breakdown-metrics', style: { fontSize: '12px' } },
            // Tokens Column
            h('div', { class: 'metric-col-tokens', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(m.total_tokens)),
              h('div', { style: { fontSize: '11px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '3px' } },
                h('span', { class: 'tok-in', title: `Fresh Input: ${fmtNum(freshM)} tokens` }, `↓ ${fmtCompact(freshM)}`),
                h('span', { class: 'muted' }, '·'),
                h('span', { class: 'muted', title: `Cache Read: ${fmtNum(m.cache_read_tokens || 0)} tokens` }, `${fmtCompact(m.cache_read_tokens || 0)}c`),
                h('span', { class: 'muted' }, '/'),
                h('span', { class: 'tok-out', title: `Output Response: ${fmtNum(m.completion_tokens)} tokens` }, `↑ ${fmtCompact(m.completion_tokens)}`)
              ),
              renderRatioBar(m.prompt_tokens, m.completion_tokens)
            ),
            // Cost Column
            h('div', { class: 'metric-col-cost', style: { textAlign: 'right', minWidth: '85px' } },
              h('div', { style: { fontSize: '13px', fontWeight: 'bold' } }, formatCost(m.estimated_cost_usd, costSettings.effective_currency, costSettings.effective_rate, m.unpriced_requests)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'est. list')
            ),
            // Requests Column
            h('div', { class: 'metric-col-requests', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } },
                fmtNum(m.total_requests),
                h('small', { class: 'muted', style: { fontSize: '11px', fontWeight: 'normal', marginLeft: '3px' } }, 'reqs')
              ),
              h('div', { style: { fontSize: '11px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '4px' } },
                h('span', { style: { color: 'var(--ok)', fontWeight: '600' }, title: `${fmtNum(m.success_requests)} successful requests` }, `✓ ${m.success_requests}`),
                m.blocked_requests > 0
                  ? h('span', { style: { color: 'var(--danger)', fontWeight: '600' }, title: `${fmtNum(m.blocked_requests)} blocked requests` }, `✕ ${m.blocked_requests}`)
                  : null
              )
            ),
            // Latency Column
            h('div', { class: 'metric-col-latency', style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '13px', fontWeight: '600' } }, fmtDur(m.avg_duration_ms)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'avg lat')
            ),
            // Share of System Column
            h('div', { class: 'metric-col-share' },
              h('div', { style: { display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '3px' } },
                h('span', { class: 'muted' }, 'Share'),
                h('span', { style: { fontWeight: '600' } }, shareVal)
              ),
              h('div', { style: { width: '100%', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                h('div', { style: { width: `${Math.min(100, Math.max(3, m.token_share || 0))}%`, height: '100%', background: 'var(--accent)', borderRadius: '3px' } })
              )
            )
          ),

          // Right: Action button
          h('div', { style: { width: '85px', display: 'flex', justifyContent: 'flex-end', alignItems: 'center' } },
            h('button', {
              class: 'btn btn-sm',
              type: 'button',
              onclick: (e) => {
                e.stopPropagation();
                setRoute('traffic', { model: m.model });
              }
            }, icon('activity'), 'View Logs')
          )
        )
      );

      // Expanded sub-table: keys that consumed this model
      if (isExpanded) {
        const consumers = [...(m.key_consumers || [])].sort((a, b) => (b.total_tokens || 0) - (a.total_tokens || 0));
        const consumerRows = consumers.map((c) => {
          const cShare = typeof c.share === 'number' ? `${c.share.toFixed(1)}%` : '-';
          const costStr = formatCost(c.estimated_cost_usd, costSettings.effective_currency, costSettings.effective_rate, c.unpriced_requests);
          const freshC = Math.max(0, (c.prompt_tokens || 0) - (c.cache_read_tokens || 0) - (c.cache_write_tokens || 0));
          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '6px' } }, icon('key')),
              c.key_name,
              c.unlinked ? h('span', { class: 'badge muted', style: { fontSize: '10.5px', marginLeft: '6px' } }, 'unlinked') : null
            ),
            h('td', null, h('code', { class: 'muted' }, c.key)),
            h('td', { class: 'num' }, fmtNum(c.requests)),
            h('td', { class: 'num tok-in' }, fmtNum(freshC)),
            h('td', { class: 'num muted' }, fmtNum(c.cache_read_tokens || 0)),
            h('td', { class: 'num tok-out' }, fmtNum(c.comp_tokens)),
            h('td', null, renderRatioBar(c.prompt_tokens, c.comp_tokens)),
            h('td', { class: 'num strong' }, fmtNum(c.total_tokens)),
            h('td', { class: 'num strong', style: { color: 'var(--text)' } }, costStr),
            h('td', { style: { minWidth: '100px' } },
              h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
                h('div', { style: { flex: '1', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                  h('div', { style: { width: `${Math.min(100, Math.max(2, c.share || 0))}%`, height: '100%', background: 'var(--ok)' } })
                ),
                h('span', { style: { fontSize: '11px', minWidth: '35px', textAlign: 'right' } }, cShare)
              )
            )
          );
        });

        const detailSection = h('div', { style: { padding: '16px 20px', background: 'var(--bg)' } },
          h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
            h('b', { style: { fontSize: '13px' } }, `Token consumers for "${m.model}"`),
            renderLastActive(m.last_active_at)
          ),
          h('div', { class: 'table-wrap', style: { background: 'var(--panel)', borderRadius: '6px', border: '1px solid var(--border)' } },
            h('table', { class: 'table' },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Client Key Name'),
                  h('th', null, 'Masked Key'),
                  h('th', { class: 'num' }, 'Requests'),
                  h('th', { class: 'num', style: { minWidth: '100px' } },
                    h('span', { class: 'tok-in' }, 'Fresh Input')
                  ),
                  h('th', { class: 'num', style: { minWidth: '95px' } },
                    h('span', { class: 'muted' }, 'Cache Read')
                  ),
                  h('th', { class: 'num', style: { minWidth: '100px' } },
                    h('span', { class: 'tok-out' }, 'Output')
                  ),
                  h('th', { style: { width: '90px' } }, 'In / Out Ratio'),
                  h('th', { class: 'num' }, 'Total Tokens'),
                  h('th', { class: 'num', style: { minWidth: '95px' } }, 'Est. Cost'),
                  h('th', { style: { minWidth: '100px' } }, 'Model Share')
                )
              ),
              h('tbody', null,
                consumerRows.length ? consumerRows : h('tr', null, h('td', { colspan: 10, class: 'muted' }, 'No consumer breakdown available.'))
              )
            )
          )
        );

        itemCard.append(detailSection);
      }

      return itemCard;
    });

    return h('div', null, legendEl, headerEl, ...cards);
  }

  function renderPluginsBreakdown() {
    const list = reportData.plugins_breakdown || [];
    if (!list.length) return null;

    const rows = list.map((pb) => h('tr', null,
      h('td', null, h('b', null, pb.plugin_id)),
      h('td', { class: 'num' }, fmtNum(pb.requests || 0)),
      h('td', { class: 'num', style: { color: 'var(--ok)', fontWeight: 'bold' } }, fmtNum(pb.tokens_saved || 0))
    ));

    return h('div', { class: 'card table-card mt', style: { marginTop: '16px' } },
      h('div', { class: 'card-head' },
        h('h3', { style: { margin: 0, fontSize: '14px' } }, 'Token Savers & Plugins Attribution'),
        h('p', { class: 'card-sub' }, 'Input tokens saved and requests processed per plugin during this period')
      ),
      h('table', { class: 'table' },
        h('thead', null,
          h('tr', null,
            h('th', null, 'Plugin ID'),
            h('th', { class: 'num' }, 'Requests Processed'),
            h('th', { class: 'num' }, 'Tokens Saved')
          )
        ),
        h('tbody', null, ...rows)
      )
    );
  }

  // ── Render Content based on active group ──
  function renderContent() {
    if (!reportData) return;
    const listEl = currentGroup === 'keys' ? renderKeysBreakdown() : renderModelsBreakdown();
    const pluginsEl = renderPluginsBreakdown();
    contentArea.replaceChildren(listEl, pluginsEl || '');
  }

  // ── Main Load ──
  async function load() {
    topKPI.style.display = 'grid';
    filterToolbar.style.display = 'flex';
    topKPI.replaceChildren(...Array.from({ length: 8 }, () => h('div', { class: 'card skel kpi-skel' })));
    contentArea.replaceChildren(h('div', { class: 'card skel', style: { height: '240px' } }));

    const queryParams = { period: currentPeriod };
    if (currentPeriod === 'custom') {
      queryParams.start = customStart;
      queryParams.end = customEnd;
    }

    try {
      reportData = await api.get('/traffic/report', queryParams);
    } catch (e) {
      if (alive) {
        topKPI.style.display = 'none';
        filterToolbar.style.display = 'none';
        contentArea.replaceChildren(
          h('div', { class: 'card table-card' },
            emptyState('alert', 'Could not load usage reports', e.message || 'Cannot reach the NineGuard server',
              h('button', {
                class: 'btn btn-primary btn-sm',
                type: 'button',
                onclick: () => load()
              }, icon('refresh'), 'Retry')
            )
          )
        );
      }
      return;
    }

    if (!alive) return;

    topKPI.style.display = 'grid';
    filterToolbar.style.display = 'flex';
    renderKPIs();
    renderContent();
  }

  root.append(h('div', { class: 'page' },
    header,
    topKPI,
    filterToolbar,
    contentArea
  ));

  load();

  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
