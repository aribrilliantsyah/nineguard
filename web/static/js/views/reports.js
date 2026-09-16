// Usage Reports: In-depth breakdowns per API Key (consumers) and per Model with custom date range.
import { api } from '../api.js';
import { h, icon, emptyState, fmtNum, fmtCompact, toast } from '../ui.js';
import { setRoute } from '../state.js';

export function mount(root) {
  let alive = true;
  let currentPeriod = 'today';
  let currentGroup = 'keys'; // 'keys' or 'models'
  let queryFilter = '';
  let customStart = '';
  let customEnd = '';
  let isCustomOpen = false;
  let reportData = null;

  // Track expanded cards
  const expandedKeys = new Set();
  const expandedModels = new Set();

  const topKPI = h('div', { class: 'grid grid-4' });
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
  const searchInput = h('div', { class: 'tool-search', style: { width: '220px' } },
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

  // ── Filter Toolbar Card ──
  const filterToolbar = h('div', { class: 'filter-card mt' },
    h('div', { class: 'filter-row' },
      h('div', { class: 'filter-group' },
        h('span', { class: 'muted', style: { fontSize: '11px', fontWeight: '600', textTransform: 'uppercase', letterSpacing: '0.04em' } }, 'Group:'),
        groupSegmented
      ),
      searchInput,
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
    onclick: () => exportCSV()
  }, icon('download'), 'Export');

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Usage Reports & Breakdown'),
      h('p', null, 'Detailed token consumption analysis, user attribution ("siapa saja pemakai tokennya"), and model usage')
    ),
    h('div', { class: 'page-actions' },
      exportBtn,
      refreshBtn
    )
  );

  function exportCSV() {
    api.download('/traffic?export=csv', {
      period: currentPeriod,
      start: currentPeriod === 'custom' ? customStart : '',
      end: currentPeriod === 'custom' ? customEnd : ''
    });
  }

  // ── KPI Helper ──
  function kpiCard(title, mainVal, subVal, ic = 'activity', badgeText = null, isWarn = false) {
    return h('div', { class: 'card stat-tile' },
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
    if (ms < 1000) return `${ms}ms`;
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

    const promptPct = totToks > 0 ? Math.round((promptToks / totToks) * 100) : 0;
    const compPct = totToks > 0 ? Math.round((compToks / totToks) * 100) : 0;

    topKPI.replaceChildren(
      kpiCard('Top Token Consumer', topKey, 'Key consuming the most tokens', 'key', 'Top Key'),
      kpiCard('Top Utilized Model', topMod, 'Model handling highest token load', 'box', 'Top Model'),
      kpiCard('Total Tokens', fmtCompact(totToks), `${fmtCompact(promptToks)} in (${promptPct}%) · ${fmtCompact(compToks)} out (${compPct}%)`, 'sparkles'),
      kpiCard('Total Requests', fmtNum(reportData.total_requests || 0), `${reportData.keys_breakdown?.length || 0} active keys · ${reportData.models_breakdown?.length || 0} models`, 'activity')
    );
  }

  // ── Render Group By Client API Key ──
  function renderKeysBreakdown() {
    let list = reportData.keys_breakdown || [];
    if (queryFilter) {
      list = list.filter(k => (k.key_name || '').toLowerCase().includes(queryFilter) || (k.key || '').toLowerCase().includes(queryFilter));
    }

    if (!list.length) {
      return emptyState('inbox', 'No API key activity found', 'Requests sent through NineGuard with client API keys will show full breakdown here.');
    }

    const cards = list.map((k) => {
      const cardId = k.key_name + '||' + k.key;
      const isExpanded = expandedKeys.has(cardId);

      const chevron = icon(isExpanded ? 'chevron-down' : 'chevron-right');
      const shareVal = typeof k.token_share === 'number' ? `${k.token_share.toFixed(1)}%` : '0%';

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
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px', minWidth: '220px' } },
            chevron,
            h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: 'bold', fontSize: '12.5px', padding: '4px 8px' } },
              icon('key'), ' ', k.key_name
            ),
            h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)
          ),

          // Middle: Metrics Pills
          h('div', { style: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '20px', fontSize: '12px' } },
            // Tokens
            h('div', { style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(k.total_tokens)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, `${fmtCompact(k.prompt_tokens)} in / ${fmtCompact(k.completion_tokens)} out`)
            ),
            // Requests
            h('div', { style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(k.total_requests)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, `${k.success_requests} ok · ${k.blocked_requests} blk`)
            ),
            // Latency
            h('div', { style: { textAlign: 'right', minWidth: '60px' } },
              h('div', { style: { fontSize: '13px', fontWeight: '600' } }, fmtDur(k.avg_duration_ms)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'avg lat')
            ),
            // Share of System
            h('div', { style: { minWidth: '90px' } },
              h('div', { style: { display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '2px' } },
                h('span', { class: 'muted' }, 'Share'),
                h('span', { style: { fontWeight: '600' } }, shareVal)
              ),
              h('div', { style: { width: '100%', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                h('div', { style: { width: `${Math.min(100, Math.max(3, k.token_share || 0))}%`, height: '100%', background: 'var(--accent)', borderRadius: '3px' } })
              )
            )
          ),

          // Right: Action button
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
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
        const models = k.model_usage || [];
        const modelRows = models.map((m) => {
          const modShare = typeof m.share === 'number' ? `${m.share.toFixed(1)}%` : '-';
          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'swatch', style: { display: 'inline-block', width: '8px', height: '8px', borderRadius: '2px', background: 'var(--lv-info)', marginRight: '6px' } }),
              m.model
            ),
            h('td', { class: 'num' }, fmtNum(m.requests)),
            h('td', { class: 'num' }, fmtNum(m.prompt_tokens)),
            h('td', { class: 'num' }, fmtNum(m.comp_tokens)),
            h('td', { class: 'num strong' }, fmtNum(m.total_tokens)),
            h('td', { style: { minWidth: '120px' } },
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
            h('span', { class: 'muted', style: { fontSize: '11px' } }, k.last_active_at ? `Last active: ${new Date(k.last_active_at).toLocaleString()}` : '')
          ),
          h('div', { class: 'table-wrap', style: { background: 'var(--panel)', borderRadius: '6px', border: '1px solid var(--border)' } },
            h('table', { class: 'table' },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Model'),
                  h('th', { class: 'num' }, 'Requests'),
                  h('th', { class: 'num' }, 'Prompt Tok'),
                  h('th', { class: 'num' }, 'Comp Tok'),
                  h('th', { class: 'num' }, 'Total Tok'),
                  h('th', null, 'Key Share')
                )
              ),
              h('tbody', null,
                modelRows.length ? modelRows : h('tr', null, h('td', { colspan: 6, class: 'muted' }, 'No model breakdown available.'))
              )
            )
          )
        );

        itemCard.append(detailSection);
      }

      return itemCard;
    });

    return h('div', null, ...cards);
  }

  // ── Render Group By Model ──
  function renderModelsBreakdown() {
    let list = reportData.models_breakdown || [];
    if (queryFilter) {
      list = list.filter(m => (m.model || '').toLowerCase().includes(queryFilter));
    }

    if (!list.length) {
      return emptyState('inbox', 'No model activity found', 'Requests routed to AI models will display token consumers and shares here.');
    }

    const cards = list.map((m) => {
      const isExpanded = expandedModels.has(m.model);
      const chevron = icon(isExpanded ? 'chevron-down' : 'chevron-right');
      const shareVal = typeof m.token_share === 'number' ? `${m.token_share.toFixed(1)}%` : '0%';

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
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px', minWidth: '220px' } },
            chevron,
            h('span', { style: { fontWeight: 'bold', fontSize: '13.5px' } }, m.model),
            m.enabled
              ? h('span', { class: 'badge ok', style: { fontSize: '10px' } }, 'Active')
              : h('span', { class: 'badge err', style: { fontSize: '10px' } }, 'Disabled')
          ),

          // Middle: Metrics Pills
          h('div', { style: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '20px', fontSize: '12px' } },
            // Tokens
            h('div', { style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(m.total_tokens)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, `${fmtCompact(m.prompt_tokens)} in / ${fmtCompact(m.completion_tokens)} out`)
            ),
            // Requests
            h('div', { style: { textAlign: 'right' } },
              h('div', { style: { fontSize: '14px', fontWeight: 'bold' } }, fmtNum(m.total_requests)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, `${m.success_requests} ok · ${m.blocked_requests} blk`)
            ),
            // Latency
            h('div', { style: { textAlign: 'right', minWidth: '60px' } },
              h('div', { style: { fontSize: '13px', fontWeight: '600' } }, fmtDur(m.avg_duration_ms)),
              h('div', { class: 'muted', style: { fontSize: '11px' } }, 'avg lat')
            ),
            // Share of System
            h('div', { style: { minWidth: '90px' } },
              h('div', { style: { display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '2px' } },
                h('span', { class: 'muted' }, 'Share'),
                h('span', { style: { fontWeight: '600' } }, shareVal)
              ),
              h('div', { style: { width: '100%', height: '5px', background: 'var(--border)', borderRadius: '3px', overflow: 'hidden' } },
                h('div', { style: { width: `${Math.min(100, Math.max(3, m.token_share || 0))}%`, height: '100%', background: 'var(--accent)', borderRadius: '3px' } })
              )
            )
          ),

          // Right: Action button
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
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

      // Expanded Sub-table: Who consumed this model ("Siapa saja pemakai model ini")
      if (isExpanded) {
        const consumers = m.key_consumers || [];
        const consumerRows = consumers.map((c) => {
          const cShare = typeof c.share === 'number' ? `${c.share.toFixed(1)}%` : '-';
          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '6px' } }, icon('key')),
              c.key_name
            ),
            h('td', null, h('code', { class: 'muted' }, c.key)),
            h('td', { class: 'num' }, fmtNum(c.requests)),
            h('td', { class: 'num' }, fmtNum(c.prompt_tokens)),
            h('td', { class: 'num' }, fmtNum(c.comp_tokens)),
            h('td', { class: 'num strong' }, fmtNum(c.total_tokens)),
            h('td', { style: { minWidth: '120px' } },
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
            h('b', { style: { fontSize: '13px' } }, `Token Consumers for "${m.model}" ("Siapa saja pemakai model ini")`),
            h('span', { class: 'muted', style: { fontSize: '11px' } }, m.last_active_at ? `Last active: ${new Date(m.last_active_at).toLocaleString()}` : '')
          ),
          h('div', { class: 'table-wrap', style: { background: 'var(--panel)', borderRadius: '6px', border: '1px solid var(--border)' } },
            h('table', { class: 'table' },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Client Key Name'),
                  h('th', null, 'Masked Key'),
                  h('th', { class: 'num' }, 'Requests'),
                  h('th', { class: 'num' }, 'Prompt Tok'),
                  h('th', { class: 'num' }, 'Comp Tok'),
                  h('th', { class: 'num' }, 'Total Tok'),
                  h('th', null, 'Model Share')
                )
              ),
              h('tbody', null,
                consumerRows.length ? consumerRows : h('tr', null, h('td', { colspan: 7, class: 'muted' }, 'No consumer breakdown available.'))
              )
            )
          )
        );

        itemCard.append(detailSection);
      }

      return itemCard;
    });

    return h('div', null, ...cards);
  }

  // ── Render Content based on active group ──
  function renderContent() {
    if (!reportData) return;
    const listEl = currentGroup === 'keys' ? renderKeysBreakdown() : renderModelsBreakdown();
    contentArea.replaceChildren(listEl);
  }

  // ── Main Load ──
  async function load() {
    topKPI.style.display = 'grid';
    filterToolbar.style.display = 'flex';
    topKPI.replaceChildren(...[1, 2, 3, 4].map(() => h('div', { class: 'card skel', style: { height: '80px' } })));
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
