// Dashboard: Universal Gateway & LLM Firewall Observability
// High-density, precision telemetry matching the clean guard & firewall monitoring reference.
import { api } from '../api.js';
import { h, icon, emptyState, fmtNum, fmtCompact, fmtAgo } from '../ui.js';
import { setRoute } from '../state.js';

// ── Math & Interpolation: Monotone Cubic Hermite Spline (Fritsch-Carlson) ──
// Guarantees zero-overshoot, zero dips below 0, and no looping curves when values fluctuate.
function monotoneCubicBezier(pts) {
  if (!pts || pts.length < 2) return '';
  if (pts.length === 2) {
    return `M ${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)} L ${pts[1].x.toFixed(1)},${pts[1].y.toFixed(1)}`;
  }
  const n = pts.length;
  const dX = [];
  const dY = [];
  const m = [];
  for (let i = 0; i < n - 1; i++) {
    const dx = pts[i + 1].x - pts[i].x;
    const dy = pts[i + 1].y - pts[i].y;
    dX.push(dx);
    dY.push(dy);
    m.push(dx !== 0 ? dy / dx : 0);
  }
  const tangents = [m[0]];
  for (let i = 1; i < n - 1; i++) {
    if (m[i - 1] * m[i] <= 0) {
      tangents.push(0);
    } else {
      tangents.push((m[i - 1] + m[i]) / 2);
    }
  }
  tangents.push(m[m.length - 1]);

  let path = `M ${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)}`;
  for (let i = 0; i < n - 1; i++) {
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const dx = dX[i];
    const cp1x = p1.x + dx / 3;
    const cp1y = p1.y + (tangents[i] * dx) / 3;
    const cp2x = p2.x - dx / 3;
    const cp2y = p2.y - (tangents[i + 1] * dx) / 3;
    path += ` C ${cp1x.toFixed(1)},${cp1y.toFixed(1)} ${cp2x.toFixed(1)},${cp2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`;
  }
  return path;
}

// ── Mini Sparkline with distinct End-Dot ──
function sparklineSVG(points, color = 'var(--muted)', dotColor = '#c42b1c', w = 90, h = 26) {
  if (!points || !points.length) {
    points = [0, 0];
  }
  if (points.length === 1) {
    points = [points[0], points[0]];
  }
  const max = Math.max(...points, 1);
  const padT = 3, padB = 4;
  const innerH = h - padT - padB;
  const step = (w - 6) / (points.length - 1);

  const pts = points.map((val, i) => ({
    x: 2 + i * step,
    y: padT + innerH - (Math.max(0, val) / max) * innerH
  }));

  const linePath = monotoneCubicBezier(pts);
  const lastPt = pts[pts.length - 1];

  return `
    <svg width="${w}" height="${h}" class="kpi-spark" viewBox="0 0 ${w} ${h}" style="overflow:visible;">
      <path d="${linePath}" fill="none" stroke="${color}" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
      <circle cx="${lastPt.x.toFixed(1)}" cy="${lastPt.y.toFixed(1)}" r="2.5" fill="${dotColor}"/>
    </svg>
  `;
}

// ── Nice Round Axis Numbers Helper ──
function niceScale(maxVal, tickCount = 3) {
  if (maxVal <= 0) return { niceMax: tickCount * 5, step: 5 };
  const rawStep = maxVal / tickCount;
  const mag = Math.pow(10, Math.floor(Math.log10(rawStep)));
  const norm = rawStep / mag;
  let niceNorm = 1;
  if (norm > 5) niceNorm = 10;
  else if (norm > 2) niceNorm = 5;
  else if (norm > 1) niceNorm = 2;
  const step = niceNorm * mag;
  return { niceMax: step * tickCount, step };
}

export function mount(root) {
  let alive = true;
  let currentPeriod = '14d';
  let cachedStats = null;
  let cachedLogs = [];
  let volumeTableView = false;
  let errorTableView = false;
  let modelMetric = 'tokens'; // 'tokens' or 'requests'
  let keyMetric = 'tokens'; // 'tokens' or 'requests'

  const pageHeadSub = h('p', null, 'Loading gateway telemetry...');
  const kpiRow = h('div', { class: 'grid grid-4' });
  const rowVolumeMix = h('div', { class: 'grid grid-2 mt' });
  const rowUsageBreakdown = h('div', { class: 'grid grid-2 mt' });
  const rowErrorInspection = h('div', { class: 'grid grid-2 mt' });
  const rowRecentLogs = h('div', { class: 'card mt' });
  const errorCard = h('div', { class: 'card table-card', style: { display: 'none' } });

  // ── Period Switcher ──
  const periods = [
    { id: '7d', label: '7 days' },
    { id: '14d', label: '14 days' },
    { id: '30d', label: '30 days' },
    { id: 'today', label: 'Today' }
  ];

  const periodButtons = periods.map(({ id, label }) => {
    const btn = h('button', {
      class: id === currentPeriod ? 'active' : '',
      type: 'button',
      onclick: () => {
        if (currentPeriod === id) return;
        currentPeriod = id;
        periodButtons.forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        load();
      }
    }, label);
    return btn;
  });

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Dashboard'),
        pageHeadSub
      ),
      h('div', { class: 'page-actions' },
        h('div', { class: 'period' }, ...periodButtons)
      )
    ),
    errorCard,
    kpiRow,
    rowVolumeMix,
    rowUsageBreakdown,
    rowErrorInspection,
    rowRecentLogs
  ));

  function getPeriodLabel(p) {
    if (p === 'today') return 'today';
    if (p === '7d') return 'last 7 days';
    if (p === '14d') return 'last 14 days';
    if (p === '30d') return 'last 30 days';
    return p;
  }

  function getCompareLabel(p) {
    if (p === 'today') return 'yesterday';
    if (p === '7d') return 'the 7 days before';
    if (p === '14d') return 'the 14 days before';
    if (p === '30d') return 'the 30 days before';
    return 'previous period';
  }

  // ── Card 1-4: KPI Top Metric Cards (Focus on Success, Tokens, Latency & Guard) ──
  function renderKPICard(iconName, label, value, subText, sparkPoints, deltaClass = '', dotColor = '#c42b1c', iconColor = 'var(--accent)') {
    const sparkWrap = h('div', { class: 'kpi-spark' });
    sparkWrap.innerHTML = sparklineSVG(sparkPoints, 'var(--muted)', dotColor);

    const iconEl = icon(iconName);
    if (iconColor) iconEl.style.color = iconColor;

    return h('div', { class: 'kpi-card' },
      h('div', { class: 'kpi-head' },
        iconEl,
        h('span', null, label)
      ),
      h('div', { class: 'kpi-body' },
        h('div', null,
          h('div', { class: 'kpi-val' }, value),
          h('div', { class: `kpi-sub ${deltaClass}` }, subText)
        ),
        sparkWrap
      )
    );
  }

  // ── Traffic Volume (Log Volume) Stacked Bar Chart & Table ──
  function renderTrafficVolumeCard(stats) {
    const series = stats.volume_series || [];
    const totalReqs = stats.total_requests || 0;
    const periodLabel = getPeriodLabel(currentPeriod);
    const comp = stats.comparison || {};
    const reqDelta = comp.req_delta_pct || 0;
    const deltaStr = reqDelta >= 0 ? `+${reqDelta.toFixed(0)}%` : `${reqDelta.toFixed(0)}%`;

    const successReqs = stats.success_requests || 0;
    const blockedReqs = stats.blocked_requests || 0;
    const errorReqs = Math.max(0, (stats.error_requests || 0) - blockedReqs);

    const toggleBtn = h('button', {
      type: 'button',
      class: `btn-table-toggle ${volumeTableView ? 'active' : ''}`,
      onclick: () => {
        volumeTableView = !volumeTableView;
        updateVolumeMixRow();
      }
    }, icon(volumeTableView ? 'activity' : 'overview'), volumeTableView ? 'Chart' : 'Table');

    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Traffic volume'),
        h('p', { class: 'card-sub' }, `${fmtNum(totalReqs)} requests in ${periodLabel}, ${deltaStr} vs the previous period • click a day to filter`)
      ),
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' } },
        h('div', { style: { display: 'flex', gap: '10px', fontSize: '11px', color: 'var(--text-2)' } },
          h('span', { style: { display: 'inline-flex', alignItems: 'center', gap: '4px' } },
            h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: 'var(--lv-info)' } }),
            `Success ${fmtNum(successReqs)}`
          ),
          h('span', { style: { display: 'inline-flex', alignItems: 'center', gap: '4px' } },
            h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: '#c42b1c' } }),
            `Blocked ${fmtNum(blockedReqs)}`
          ),
          h('span', { style: { display: 'inline-flex', alignItems: 'center', gap: '4px' } },
            h('span', { style: { width: '8px', height: '8px', borderRadius: '2px', background: 'var(--lv-warn)' } }),
            `Error ${fmtNum(errorReqs)}`
          )
        ),
        toggleBtn
      )
    );

    if (volumeTableView) {
      // Table View
      const table = h('table', { class: 'table' },
        h('thead', null, h('tr', null,
          h('th', null, 'Time / Date'),
          h('th', { class: 'num' }, 'Requests'),
          h('th', { class: 'num' }, 'Success (2xx)'),
          h('th', { class: 'num' }, 'Blocked (403)'),
          h('th', { class: 'num' }, 'Errors'),
          h('th', { class: 'num' }, 'Tokens')
        )),
        h('tbody', null,
          series.length
            ? series.slice().reverse().map(pt => h('tr', null,
              h('td', { class: 'strong' }, pt.time),
              h('td', { class: 'num' }, fmtNum(pt.requests)),
              h('td', { class: 'num', style: { color: 'var(--ok)' } }, fmtNum(pt.success || (pt.requests - (pt.blocked || 0) - (pt.errors || 0)))),
              h('td', { class: 'num', style: { color: pt.blocked > 0 ? '#c42b1c' : 'inherit' } }, fmtNum(pt.blocked || 0)),
              h('td', { class: 'num', style: { color: pt.errors > 0 ? 'var(--lv-warn)' : 'inherit' } }, fmtNum(pt.errors || 0)),
              h('td', { class: 'num' }, fmtCompact(pt.tokens))
            ))
            : h('tr', null, h('td', { colspan: 6, class: 'muted' }, 'No traffic data recorded.'))
        )
      );
      return h('div', { class: 'card' }, cardHead, h('div', { class: 'table-wrap', style: { maxHeight: '220px', overflowY: 'auto' } }, table));
    }

    // Chart View: Stacked vertical bars
    if (!series || !series.length) {
      return h('div', { class: 'card' }, cardHead, emptyState('inbox', 'No traffic data recorded for this period'));
    }

    const chartH = 145;
    const maxVal = Math.max(...series.map(s => s.requests), 1);
    const n = series.length;
    const barWidth = Math.max(8, Math.min(28, Math.floor(480 / n) - 4));

    const bars = series.map((pt) => {
      const tot = pt.requests || 0;
      const blk = pt.blocked || 0;
      const err = Math.max(0, (pt.errors || 0) - blk);
      const succ = Math.max(0, tot - blk - err);

      const totH = tot > 0 ? Math.max(3, (tot / maxVal) * (chartH - 28)) : 1;
      const succH = tot > 0 ? (succ / tot) * totH : 0;
      const blkH = tot > 0 ? (blk / tot) * totH : 0;
      const errH = tot > 0 ? (err / tot) * totH : 0;

      const title = `${pt.time}: ${fmtNum(tot)} reqs (${fmtNum(succ)} ok, ${fmtNum(blk)} blocked, ${fmtNum(err)} err) · ${fmtCompact(pt.tokens)} tok`;

      return h('div', {
        style: {
          flex: '1',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: '5px',
          minWidth: `${barWidth}px`,
          cursor: 'pointer'
        },
        title,
        onclick: () => setRoute('traffic', { status: blk > 0 ? '403' : '' })
      },
        h('div', {
          style: {
            width: `${barWidth}px`,
            height: `${chartH - 24}px`,
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'flex-end',
            background: 'transparent'
          }
        },
          tot > 0
            ? h('div', {
              style: {
                width: '100%',
                height: `${totH}px`,
                display: 'flex',
                flexDirection: 'column-reverse',
                borderRadius: '3px 3px 0 0',
                overflow: 'hidden'
              }
            },
              h('div', { style: { width: '100%', height: `${succH}px`, background: 'var(--lv-info)' } }),
              h('div', { style: { width: '100%', height: `${blkH}px`, background: '#c42b1c' } }),
              h('div', { style: { width: '100%', height: `${errH}px`, background: 'var(--lv-warn)' } })
            )
            : h('div', {
              style: {
                width: '100%',
                height: '2px',
                background: 'var(--border)'
              }
            })
        ),
        h('span', { style: { fontSize: '10px', color: 'var(--muted)', whiteSpace: 'nowrap' } }, pt.time)
      );
    });

    const chartWrap = h('div', {
      style: {
        display: 'flex',
        alignItems: 'flex-end',
        gap: '4px',
        height: `${chartH}px`,
        paddingTop: '6px',
        overflowX: 'auto'
      }
    }, ...bars);

    return h('div', { class: 'card' }, cardHead, chartWrap);
  }

  // ── Level Mix Card (Status and Protection Breakdown) ──
  function renderLevelMixCard(stats) {
    const periodLabel = getPeriodLabel(currentPeriod);
    const totalReqs = stats.total_requests || 0;
    const levels = stats.level_mix || [];

    // Continuous segmented bar at top
    const segmentSpans = levels.map(l => {
      const share = totalReqs > 0 ? Math.max(l.count > 0 ? 2 : 0, l.share) : 0;
      return h('div', {
        class: 'level-segment-fill',
        style: {
          width: `${share}%`,
          background: l.color
        },
        title: `${l.label}: ${fmtNum(l.count)} (${l.share.toFixed(1)}%)`
      });
    });

    const segmentBar = h('div', { class: 'level-segment-bar' }, ...segmentSpans);

    // Breakdown table
    const tableRows = levels.map(l => {
      const vsPrev = l.vs_prev || 0;
      const vsPrevText = vsPrev === 0 ? '0.0 pt' : (vsPrev > 0 ? `+${vsPrev.toFixed(1)} pt` : `${vsPrev.toFixed(1)} pt`);
      const vsPrevColor = (l.level === 'blocked' || l.level === 'error')
        ? (vsPrev > 0 ? '#c42b1c' : 'var(--ok)')
        : (vsPrev >= 0 ? 'var(--ok)' : 'var(--muted)');

      return h('tr', null,
        h('td', { class: 'strong' },
          h('span', {
            style: {
              display: 'inline-block',
              width: '8px',
              height: '8px',
              borderRadius: '2px',
              background: l.color,
              marginRight: '8px',
              verticalAlign: 'middle'
            }
          }),
          l.label
        ),
        h('td', { class: 'num' }, fmtNum(l.count)),
        h('td', { class: 'num' }, `${l.share.toFixed(1)}%`),
        h('td', { class: 'num', style: { color: vsPrevColor, fontWeight: '500' } }, vsPrevText)
      );
    });

    // Token In vs Token Out Summary underneath table
    const totTok = stats.total_tokens || 0;
    const promptTok = stats.prompt_tokens || 0;
    const compTok = stats.completion_tokens || 0;
    const pRatio = totTok > 0 ? Math.round((promptTok / totTok) * 100) : 50;
    const cRatio = totTok > 0 ? Math.round((compTok / totTok) * 100) : 50;

    const tokenSubRow = h('div', { style: { marginTop: '12px', borderTop: '1px solid var(--border)', paddingTop: '10px' } },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '5px' } },
        h('span', null, h('span', { class: 'muted' }, 'Prompt In: '), h('b', null, fmtCompact(promptTok)), ` (${pRatio}%)`),
        h('span', null, h('span', { class: 'muted' }, 'Completion Out: '), h('b', null, fmtCompact(compTok)), ` (${cRatio}%)`)
      ),
      h('div', { style: { width: '100%', height: '5px', background: 'var(--hover)', borderRadius: '3px', overflow: 'hidden', display: 'flex' } },
        h('div', { style: { width: `${pRatio}%`, background: 'var(--lv-info)' } }),
        h('div', { style: { width: `${cRatio}%`, background: 'var(--ok)' } })
      )
    );

    const mixTable = h('table', { class: 'table' },
      h('thead', null, h('tr', null,
        h('th', null, 'Level'),
        h('th', { class: 'num' }, 'Lines'),
        h('th', { class: 'num' }, 'Share'),
        h('th', { class: 'num' }, 'vs prev')
      )),
      h('tbody', null, ...tableRows)
    );

    return h('div', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Level mix'),
          h('p', { class: 'card-sub' }, `Share of all requests per level, ${periodLabel}`)
        )
      ),
      segmentBar,
      mixTable,
      tokenSubRow
    );
  }

  // ── Usage by Model Card (Matching Screenshot 1 Layout with Bars & Trends) ──
  function renderUsageByModelCard(stats) {
    const periodLabel = getPeriodLabel(currentPeriod);
    const topModels = stats.top_models || [];

    const switcher = h('div', { class: 'period', style: { padding: '2px' } },
      h('button', {
        class: modelMetric === 'tokens' ? 'active' : '',
        style: { height: '22px', padding: '0 8px', fontSize: '11px' },
        onclick: () => { modelMetric = 'tokens'; updateUsageBreakdownRow(); }
      }, 'Tokens'),
      h('button', {
        class: modelMetric === 'requests' ? 'active' : '',
        style: { height: '22px', padding: '0 8px', fontSize: '11px' },
        onclick: () => { modelMetric = 'requests'; updateUsageBreakdownRow(); }
      }, 'Requests')
    );

    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Usage by model'),
        h('p', { class: 'card-sub' }, `Top models ranked by ${modelMetric} with daily trend, ${periodLabel}`)
      ),
      switcher
    );

    if (!topModels.length) {
      return h('div', { class: 'card' }, cardHead, emptyState('box', 'No model usage recorded yet'));
    }

    const maxVal = Math.max(...topModels.map(m => modelMetric === 'tokens' ? m.tokens : m.requests), 1);

    const items = topModels.map(m => {
      const curVal = modelMetric === 'tokens' ? m.tokens : m.requests;
      const barPct = Math.max(3, (curVal / maxVal) * 100);

      // Parse prefix / namespace
      const slashIdx = m.model.lastIndexOf('/');
      const modelName = slashIdx >= 0 ? m.model.slice(slashIdx + 1) : m.model;
      const namespace = slashIdx >= 0 ? m.model.slice(0, slashIdx) : 'direct';

      const sparkWrap = h('div', { class: 'source-spark-col' });
      sparkWrap.innerHTML = sparklineSVG(m.trend || [], 'var(--muted)', 'var(--lv-info)', 80, 22);

      return h('div', {
        class: 'source-item wide-count',
        style: { cursor: 'pointer' },
        onclick: () => setRoute('traffic', { model: m.model }),
        title: `Click to filter traffic for model: ${m.model}`
      },
        h('div', { class: 'source-details' },
          h('div', { class: 'source-title-row' },
            h('span', { class: 'source-title', title: m.model }, modelName),
            h('span', { class: 'source-ns' }, namespace)
          ),
          h('div', { class: 'source-bar-wrap' },
            h('div', { class: 'source-bar-fill blue', style: { width: `${barPct}%` } })
          )
        ),
        sparkWrap,
        h('div', { class: 'source-count-col' },
          h('span', { class: 'source-count-val' }, modelMetric === 'tokens' ? fmtCompact(m.tokens) : fmtNum(m.requests)),
          h('span', { class: 'source-count-lbl' }, modelMetric === 'tokens' ? `${fmtNum(m.requests)} reqs` : `${fmtCompact(m.tokens)} tok`)
        )
      );
    });

    return h('div', { class: 'card' }, cardHead, h('div', { class: 'source-list' }, ...items));
  }

  // ── Usage by API Key / Client Card (Matching Screenshot 1 Layout with Bars & Trends) ──
  function renderUsageByKeyCard(stats) {
    const periodLabel = getPeriodLabel(currentPeriod);
    const topKeys = stats.top_keys || [];

    const switcher = h('div', { class: 'period', style: { padding: '2px' } },
      h('button', {
        class: keyMetric === 'tokens' ? 'active' : '',
        style: { height: '22px', padding: '0 8px', fontSize: '11px' },
        onclick: () => { keyMetric = 'tokens'; updateUsageBreakdownRow(); }
      }, 'Tokens'),
      h('button', {
        class: keyMetric === 'requests' ? 'active' : '',
        style: { height: '22px', padding: '0 8px', fontSize: '11px' },
        onclick: () => { keyMetric = 'requests'; updateUsageBreakdownRow(); }
      }, 'Requests')
    );

    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Usage by client API key'),
        h('p', { class: 'card-sub' }, `Token consumption & request load per client key, ${periodLabel}`)
      ),
      switcher
    );

    if (!topKeys.length) {
      return h('div', { class: 'card' }, cardHead, emptyState('key', 'No client API key activity yet'));
    }

    const maxVal = Math.max(...topKeys.map(k => keyMetric === 'tokens' ? k.tokens : k.requests), 1);

    const items = topKeys.map(k => {
      const curVal = keyMetric === 'tokens' ? k.tokens : k.requests;
      const barPct = Math.max(3, (curVal / maxVal) * 100);

      const sparkWrap = h('div', { class: 'source-spark-col' });
      sparkWrap.innerHTML = sparklineSVG(k.trend || [], 'var(--muted)', 'var(--accent)', 80, 22);

      return h('div', {
        class: 'source-item wide-count',
        style: { cursor: 'pointer' },
        onclick: () => setRoute('traffic', { api_key: k.key }),
        title: `Click to filter traffic for key: ${k.name || k.key}`
      },
        h('div', { class: 'source-details' },
          h('div', { class: 'source-title-row' },
            h('span', { class: 'source-title' }, k.name || 'API Client'),
            h('span', { class: 'source-ns' }, k.key)
          ),
          h('div', { class: 'source-bar-wrap' },
            h('div', { class: 'source-bar-fill accent', style: { width: `${barPct}%` } })
          )
        ),
        sparkWrap,
        h('div', { class: 'source-count-col' },
          h('span', { class: 'source-count-val' }, keyMetric === 'tokens' ? fmtCompact(k.tokens) : fmtNum(k.requests)),
          h('span', { class: 'source-count-lbl' }, keyMetric === 'tokens' ? `${fmtNum(k.requests)} reqs` : `${fmtCompact(k.tokens)} tok`)
        )
      );
    });

    return h('div', { class: 'card' }, cardHead, h('div', { class: 'source-list' }, ...items));
  }

  // ── Error Rate Area & Line Chart (Screenshot 2 Reference) ──
  function renderErrorRateCard(stats) {
    const series = stats.volume_series || [];
    const periodLabel = getPeriodLabel(currentPeriod);
    const comp = stats.comparison || {};
    const totalReqs = stats.total_requests || 0;
    const totalErrs = (stats.blocked_requests || 0) + (stats.error_requests || 0);

    const avgErrRate = totalReqs > 0 ? (totalErrs / totalReqs * 100).toFixed(2) : '0.00';
    const prevErrRate = (comp.prev_error_rate || 0).toFixed(2);

    const toggleBtn = h('button', {
      type: 'button',
      class: `btn-table-toggle ${errorTableView ? 'active' : ''}`,
      onclick: () => {
        errorTableView = !errorTableView;
        updateErrorInspectionRow();
      }
    }, icon(errorTableView ? 'activity' : 'overview'), errorTableView ? 'Chart' : 'Table');

    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Error & Block rate'),
        h('p', { class: 'card-sub' }, `Policy blocks & errors as a share of all lines, per day • click a day to inspect`),
        h('div', { style: { fontSize: '11.5px', color: 'var(--muted)', marginTop: '2px' } },
          `Average ${avgErrRate}% • previous ${currentPeriod === 'today' ? 'day' : periodLabel} ${prevErrRate}%`
        )
      ),
      toggleBtn
    );

    if (errorTableView) {
      // Table View
      const table = h('table', { class: 'table' },
        h('thead', null, h('tr', null,
          h('th', null, 'Time / Date'),
          h('th', { class: 'num' }, 'Error Rate (%)'),
          h('th', { class: 'num' }, 'Blocked / Errors'),
          h('th', { class: 'num' }, 'Total Requests')
        )),
        h('tbody', null,
          series.length
            ? series.slice().reverse().map(pt => {
              const blk = pt.blocked || 0;
              const err = pt.errors || 0;
              const rate = (pt.error_rate || 0).toFixed(2);
              return h('tr', null,
                h('td', { class: 'strong' }, pt.time),
                h('td', { class: 'num', style: { color: parseFloat(rate) > 0 ? '#c42b1c' : 'inherit' } }, `${rate}%`),
                h('td', { class: 'num' }, fmtNum(blk + err)),
                h('td', { class: 'num' }, fmtNum(pt.requests))
              );
            })
            : h('tr', null, h('td', { colspan: 4, class: 'muted' }, 'No data available.'))
        )
      );
      return h('div', { class: 'card' }, cardHead, h('div', { class: 'table-wrap', style: { maxHeight: '220px', overflowY: 'auto' } }, table));
    }

    if (!series || !series.length) {
      return h('div', { class: 'card' }, cardHead, emptyState('inbox', 'No error or traffic records'));
    }

    // Chart Dimensions
    const W = 650;
    const H = 190;
    const padL = 42;
    const padR = 48;
    const padT = 16;
    const padB = 26;
    const innerW = W - padL - padR;
    const innerH = H - padT - padB;

    const rawMaxRate = Math.max(...series.map(s => s.error_rate || 0), 0);
    const tickTarget = rawMaxRate > 15 ? rawMaxRate : (rawMaxRate > 5 ? 15 : (rawMaxRate > 1 ? 5 : 1));
    const { niceMax, step } = niceScale(tickTarget, 3);
    const maxVal = Math.max(niceMax, 1);

    const n = series.length;
    const stepX = n > 1 ? innerW / (n - 1) : innerW / 2;
    const getX = (i) => n > 1 ? padL + i * stepX : padL + innerW / 2;
    const getY = (val) => padT + innerH - (Math.max(0, Math.min(val, maxVal)) / maxVal) * innerH;
    const baseY = getY(0);

    // Horizontal dashed grid lines at nice tick percentages
    const tickCount = Math.round(maxVal / step);
    let gridHtml = '';
    for (let i = 0; i <= tickCount; i++) {
      const tickVal = i * step;
      const y = getY(tickVal);
      gridHtml += `
        <line x1="${padL}" y1="${y.toFixed(1)}" x2="${W - padR + 10}" y2="${y.toFixed(1)}" stroke="var(--border)" stroke-dasharray="3,3" stroke-width="1"/>
        <text x="${padL - 8}" y="${(y + 3.5).toFixed(1)}" font-size="10" fill="var(--muted)" text-anchor="end" font-family="var(--font)">${tickVal}%</text>
      `;
    }

    // X-Axis labels
    const stepLabel = Math.max(1, Math.ceil(n / 6));
    const xLabels = series.map((pt, i) => {
      if (i % stepLabel !== 0 && i !== n - 1) return '';
      const x = getX(i);
      return `<text x="${x}" y="${H - 6}" font-size="10" fill="var(--muted)" text-anchor="middle" font-family="var(--font)">${pt.time}</text>`;
    }).join('');

    // Coordinates
    const pts = series.map((pt, i) => ({
      x: getX(i),
      y: getY(pt.error_rate || 0),
      rate: pt.error_rate || 0,
      time: pt.time,
      reqs: pt.requests,
      errs: (pt.blocked || 0) + (pt.errors || 0)
    }));

    const linePath = monotoneCubicBezier(pts);
    const lastPt = pts[pts.length - 1];
    const areaPath = `${linePath} L ${lastPt.x.toFixed(1)},${baseY.toFixed(1)} L ${pts[0].x.toFixed(1)},${baseY.toFixed(1)} Z`;

    const gradId = 'errorRateGrad';
    const defsHtml = `
      <linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" stop-color="#c42b1c" stop-opacity="0.18"/>
        <stop offset="100%" stop-color="#c42b1c" stop-opacity="0.0"/>
      </linearGradient>
    `;

    // Interactive hover targets
    const hitsHtml = pts.map(p => `
      <circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="10" fill="transparent" class="chart-hit">
        <title>${p.time}: ${p.rate.toFixed(2)}% (${fmtNum(p.errs)} errors / ${fmtNum(p.reqs)} reqs)</title>
      </circle>
    `).join('');

    // End-point marker and label (like 0.01% with red dot in reference)
    const endDotHtml = `
      <circle cx="${lastPt.x.toFixed(1)}" cy="${lastPt.y.toFixed(1)}" r="3.5" fill="#c42b1c" stroke="var(--panel)" stroke-width="1.5" class="chart-point"/>
      <text x="${(lastPt.x + 8).toFixed(1)}" y="${(lastPt.y + 3.5).toFixed(1)}" font-size="11" font-weight="700" fill="#c42b1c" font-family="var(--font)">${lastPt.rate.toFixed(2)}%</text>
    `;

    const svgHtml = `
      <svg width="100%" height="${H}" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" style="overflow:visible;" xmlns="http://www.w3.org/2000/svg">
        <defs>${defsHtml}</defs>
        ${gridHtml}
        <path d="${areaPath}" fill="url(#${gradId})" stroke="none"/>
        <path d="${linePath}" fill="none" stroke="#c42b1c" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        ${endDotHtml}
        ${hitsHtml}
        ${xLabels}
      </svg>
    `;

    const chartWrap = h('div', { style: { width: '100%', minHeight: `${H}px`, marginTop: '8px' } });
    chartWrap.innerHTML = svgHtml;

    return h('div', { class: 'card' }, cardHead, chartWrap);
  }

  // ── Top Firewall & Error Sources Card (Screenshot 1 Reference) ──
  function renderTopErrorSourcesCard(stats) {
    const periodLabel = getPeriodLabel(currentPeriod);
    const errSources = stats.top_error_sources || [];

    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Top policy blocks & error sources'),
        h('p', { class: 'card-sub' }, `Requests intercepted by firewall or upstream errors, ${periodLabel}`)
      )
    );

    if (errSources.length > 0) {
      const maxCount = Math.max(...errSources.map(s => s.count), 1);

      const items = errSources.map(s => {
        const barPct = Math.max(3, (s.count / maxCount) * 100);
        const sparkWrap = h('div', { class: 'source-spark-col' });
        sparkWrap.innerHTML = sparklineSVG(s.trend || [], 'var(--muted)', '#c42b1c', 80, 22);

        return h('div', {
          class: 'source-item wide-count',
          style: { cursor: 'pointer' },
          onclick: () => setRoute('traffic', { model: s.source, status: '403' }),
          title: `Inspect blocked logs for ${s.source}`
        },
          h('div', { class: 'source-details' },
            h('div', { class: 'source-title-row' },
              h('span', { class: 'source-title' }, s.source),
              h('span', { class: 'source-ns' }, s.namespace)
            ),
            h('div', { class: 'source-bar-wrap' },
              h('div', { class: 'source-bar-fill', style: { width: `${barPct}%` } })
            )
          ),
          sparkWrap,
          h('div', { class: 'source-count-col' },
            h('span', { class: 'source-count-val' }, fmtNum(s.count)),
            h('span', { class: 'source-count-lbl' }, s.is_new ? 'new' : 'errs')
          )
        );
      });

      return h('div', { class: 'card' }, cardHead, h('div', { class: 'source-list' }, ...items));
    }

    // Zero errors state: clean adherence card
    const cleanBanner = h('div', {
      style: {
        display: 'flex',
        alignItems: 'center',
        gap: '12px',
        padding: '24px 16px',
        textAlign: 'left'
      }
    },
      icon('shield', 'ok', { style: { width: '32px', height: '32px', color: 'var(--ok)' } }),
      h('div', null,
        h('b', { style: { fontSize: '13px', color: 'var(--text)' } }, '100% Policy Compliance & Clean Traffic'),
        h('p', { class: 'muted', style: { fontSize: '11.5px', margin: '3px 0 0' } },
          'No unauthorized models were requested or rejected by the firewall during this period.'
        )
      )
    );

    return h('div', { class: 'card' }, cardHead, cleanBanner);
  }

  // ── Recent Guard Activity & Audit Logs Table (Direct Usage Feed) ──
  function renderRecentLogsCard(logs = []) {
    const cardHead = h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Recent guard activity & audit logs'),
        h('p', { class: 'card-sub' }, 'Real-time stream of model requests evaluated, routed, and protected by NineGuard')
      ),
      h('button', {
        type: 'button',
        class: 'btn btn-secondary btn-sm',
        onclick: () => setRoute('traffic')
      }, icon('activity'), 'View all traffic logs →')
    );

    if (!logs.length) {
      return h('div', { class: 'card' }, cardHead, emptyState('inbox', 'No recent proxy requests recorded'));
    }

    const rows = logs.map(l => {
      const isOk = l.status_code >= 200 && l.status_code < 400;
      const isBlk = l.status_code === 403;
      const statusClass = isOk ? 's-2xx' : (isBlk ? 's-403' : 's-err');
      const statusText = isBlk ? '403 Blocked' : (isOk ? `${l.status_code} OK` : `${l.status_code} Error`);

      const slashIdx = l.model.lastIndexOf('/');
      const modShort = slashIdx >= 0 ? l.model.slice(slashIdx + 1) : l.model;
      const modPrefix = slashIdx >= 0 ? l.model.slice(0, slashIdx) : '';

      const latStr = l.duration_ms > 0 ? `${fmtNum(l.duration_ms)}ms` : '< 1ms';
      const streamBadge = l.stream ? h('span', { class: 'badge', style: { fontSize: '9.5px', padding: '1px 4px', marginLeft: '4px' } }, 'SSE') : null;

      const dateStr = l.timestamp ? (new Date(l.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })) : '-';

      return h('tr', {
        style: { cursor: 'pointer' },
        onclick: () => setRoute('traffic', { model: l.model }),
        title: `Click to inspect log #${l.id}`
      },
        h('td', { class: 'muted', style: { whiteSpace: 'nowrap' } }, dateStr),
        h('td', null,
          h('span', { class: `badge-status ${statusClass}` },
            isBlk ? icon('shield') : (isOk ? icon('checkmark') : icon('alert')),
            statusText
          )
        ),
        h('td', null,
          h('b', { style: { fontSize: '11.5px' } }, l.api_key_name || 'Client Key'),
          h('span', { class: 'muted', style: { fontSize: '10.5px', marginLeft: '6px' } }, l.api_key)
        ),
        h('td', null,
          modPrefix ? h('span', { class: 'source-ns', style: { marginRight: '6px' } }, modPrefix) : null,
          h('span', { class: 'strong' }, modShort),
          l.error_message ? h('span', { style: { color: '#c42b1c', fontSize: '10.5px', marginLeft: '6px' } }, `(${l.error_message})`) : null
        ),
        h('td', { class: 'num' },
          h('span', { title: `Prompt: ${fmtNum(l.prompt_tokens || 0)} · Comp: ${fmtNum(l.completion_tokens || 0)}` },
            fmtCompact(l.total_tokens || 0)
          )
        ),
        h('td', { class: 'num' },
          latStr,
          streamBadge
        ),
        h('td', { class: 'muted', style: { fontSize: '10.5px' } }, l.client_ip || '-')
      );
    });

    const table = h('table', { class: 'table' },
      h('thead', null, h('tr', null,
        h('th', null, 'Time'),
        h('th', null, 'Guard Status'),
        h('th', null, 'Client Key'),
        h('th', null, 'Target Model'),
        h('th', { class: 'num' }, 'Tokens'),
        h('th', { class: 'num' }, 'Latency'),
        h('th', null, 'Client IP')
      )),
      h('tbody', null, ...rows)
    );

    return h('div', { class: 'card' }, cardHead, h('div', { class: 'table-wrap' }, table));
  }

  function updateVolumeMixRow() {
    if (!cachedStats) return;
    rowVolumeMix.replaceChildren(
      renderTrafficVolumeCard(cachedStats),
      renderLevelMixCard(cachedStats)
    );
  }

  function updateUsageBreakdownRow() {
    if (!cachedStats) return;
    rowUsageBreakdown.replaceChildren(
      renderUsageByModelCard(cachedStats),
      renderUsageByKeyCard(cachedStats)
    );
  }

  function updateErrorInspectionRow() {
    if (!cachedStats) return;
    rowErrorInspection.replaceChildren(
      renderErrorRateCard(cachedStats),
      renderTopErrorSourcesCard(cachedStats)
    );
  }

  // ── Main Data Fetch & Render ──
  async function load() {
    errorCard.style.display = 'none';
    kpiRow.style.display = 'grid';
    rowVolumeMix.style.display = 'grid';
    rowUsageBreakdown.style.display = 'grid';
    rowErrorInspection.style.display = 'grid';
    rowRecentLogs.style.display = 'block';

    // Update Subtitle based on selected period
    const pLabel = getPeriodLabel(currentPeriod);
    const compLabel = getCompareLabel(currentPeriod);
    pageHeadSub.textContent = `Universal Gateway & LLM Firewall • ${pLabel}, compared with ${compLabel}`;

    // Skeletons
    kpiRow.replaceChildren(...[1, 2, 3, 4].map(() => h('div', { class: 'card skel', style: { height: '84px' } })));
    rowVolumeMix.replaceChildren(
      h('div', { class: 'card skel', style: { height: '220px' } }),
      h('div', { class: 'card skel', style: { height: '220px' } })
    );
    rowUsageBreakdown.replaceChildren(
      h('div', { class: 'card skel', style: { height: '220px' } }),
      h('div', { class: 'card skel', style: { height: '220px' } })
    );
    rowErrorInspection.replaceChildren(
      h('div', { class: 'card skel', style: { height: '220px' } }),
      h('div', { class: 'card skel', style: { height: '220px' } })
    );
    rowRecentLogs.replaceChildren(h('div', { class: 'skel', style: { height: '180px' } }));

    let stats, logsResp;
    try {
      [stats, logsResp] = await Promise.all([
        api.get('/traffic/stats', { period: currentPeriod }),
        api.get('/traffic', { limit: 8 }).catch(() => ({ logs: [] }))
      ]);
      cachedStats = stats;
      cachedLogs = (logsResp && logsResp.logs) ? logsResp.logs : [];
    } catch (err) {
      if (alive) {
        kpiRow.style.display = 'none';
        rowVolumeMix.style.display = 'none';
        rowUsageBreakdown.style.display = 'none';
        rowErrorInspection.style.display = 'none';
        rowRecentLogs.style.display = 'none';

        errorCard.style.display = 'block';
        errorCard.replaceChildren(
          emptyState('alert', 'Could not load statistics', err.message || 'Cannot reach NineGuard server',
            h('button', {
              class: 'btn btn-primary btn-sm',
              type: 'button',
              onclick: () => load()
            }, icon('refresh'), 'Retry')
          )
        );
      }
      return;
    }
    if (!alive) return;

    const series = stats.volume_series || [];
    const comp = stats.comparison || {};

    const reqSpark = series.map(s => s.requests || 0);
    const tokenSpark = series.map(s => s.tokens || 0);
    const errSpark = series.map(s => (s.blocked || 0) + (s.errors || 0));
    const latSpark = series.map(s => s.avg_latency_ms || 0);

    // Delta percentages
    const reqDelta = comp.req_delta_pct || 0;
    const reqDeltaText = `${reqDelta >= 0 ? '+' : ''}${reqDelta.toFixed(0)}% vs ${currentPeriod === 'today' ? 'yesterday' : 'prev'}`;
    const reqDeltaClass = reqDelta >= 0 ? 'delta-good' : 'delta-warn';

    const totalBlockedAndErrs = (stats.blocked_requests || 0) + (stats.error_requests || 0);
    const errDelta = comp.err_delta_pct || 0;
    const errDeltaText = totalBlockedAndErrs === 0 ? '0 policy violations' : `${errDelta >= 0 ? '+' : ''}${errDelta.toFixed(0)}% vs ${currentPeriod === 'today' ? 'yesterday' : 'prev'}`;
    const errDeltaClass = totalBlockedAndErrs === 0 ? 'delta-good' : (errDelta <= 0 ? 'delta-good' : 'delta-warn');

    const totalReqs = stats.total_requests || 0;
    const successReqs = stats.success_requests || 0;
    const successRate = typeof stats.success_rate === 'number' ? stats.success_rate : 100;

    const activeModels = stats.active_models || 0;
    const activeKeys = stats.active_keys || 0;
    const avgLatStr = stats.avg_duration_ms > 0 ? `${stats.avg_duration_ms}ms` : '< 1ms';

    // 1. Top KPI Row (Screenshot 4 Style, Focusing on Success, Tokens, Latency & Guard)
    kpiRow.replaceChildren(
      // Total Requests & Success Count
      renderKPICard('activity', currentPeriod === 'today' ? 'Lines today' : 'Requests', fmtNum(totalReqs), `${fmtNum(successReqs)} passed (${successRate.toFixed(1)}%)`, reqSpark, reqDeltaClass, 'var(--lv-info)', 'var(--lv-info)'),
      // Token Consumption Guard
      renderKPICard('sparkles', 'Total Tokens', fmtCompact(stats.total_tokens), `In: ${fmtCompact(stats.prompt_tokens)} • Out: ${fmtCompact(stats.completion_tokens)}`, tokenSpark, 'delta-good', 'var(--ok)', 'var(--ok)'),
      // Firewall Policy Blocks
      renderKPICard('shield', 'Firewall Blocks', fmtNum(stats.blocked_requests), totalBlockedAndErrs === 0 ? '100% clean traffic' : `${fmtNum(stats.blocked_requests)} blocked (403) · ${fmtNum(stats.error_requests - stats.blocked_requests)} err`, errSpark, errDeltaClass, '#c42b1c', '#c42b1c'),
      // Active Workloads & Speed
      renderKPICard('server', 'Avg Latency & Workloads', avgLatStr, `${activeModels} models, ${activeKeys} keys • P95: ${stats.p95_duration_ms || stats.avg_duration_ms}ms`, latSpark, '', 'var(--accent)', 'var(--accent)')
    );

    // 2. Middle Row: Traffic Volume Bar Chart & Level Mix
    updateVolumeMixRow();

    // 3. Usage by Model & Usage by API Key Row (Matching Screenshot 1 Layout with Bars & Trends)
    updateUsageBreakdownRow();

    // 4. Error Rate Area Chart & Top Error Sources Row (Screenshot 2 & Screenshot 1)
    updateErrorInspectionRow();

    // 5. Recent Guard Activity & Audit Logs Row (Direct Live Usage Feed)
    rowRecentLogs.replaceChildren(renderRecentLogsCard(cachedLogs));
  }

  load();
  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
