// Charts as inline SVG drawn at the real pixel width: stacked level volume,
// single-series line, sparkline and a part-to-whole bar. Volume and line
// charts have a hover/keyboard tooltip and a table view, so no value is
// reachable only by hovering. Text always uses text colors; only marks carry
// series colors.
import { h, s, icon, fmtNum, fmtCompact, fmtClock, localDate } from './ui.js';

// Bottom to top. FATAL is folded into Error: five levels would need a fifth
// hue that fails the colorblind-separation check next to red.
export const SERIES = [
  { label: 'Error', levels: ['ERROR', 'FATAL'], color: 'var(--lv-error)' },
  { label: 'Warn', levels: ['WARN'], color: 'var(--lv-warn)' },
  { label: 'Info', levels: ['INFO'], color: 'var(--lv-info)' },
  { label: 'Debug', levels: ['DEBUG'], color: 'var(--lv-debug)' },
];
const AXIS_H = 18; // x-axis label band, part of the chart height
const GAP = 2; // surface gap between stacked segments and between bars
const BAR_MAX = 24;
const CHAR_W = 6.2; // axis text is 10px monospace

export const sumLevels = (counts, levels) => levels.reduce((a, l) => a + ((counts || {})[l] || 0), 0);
export const fmtPct = (v, digits = 1) => `${(v || 0).toFixed(digits).replace(/\.0+$/, '')}%`;

// Round axis ticks from 0 that cover max in about `count` steps.
export function niceTicks(max, count = 3) {
  if (!(max > 0)) return [0, 1];
  const raw = max / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((x) => x >= raw);
  const out = [];
  for (let v = 0; v < max + step * 0.999; v += step) out.push(+v.toPrecision(12));
  return out;
}

// Rect with rounded top corners only (data end rounded, baseline square).
function topRounded(x, y, w, hgt, r) {
  r = Math.min(r, w / 2, hgt);
  return `M${x},${y + hgt}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + hgt}Z`;
}

// Tooltip row: the value leads, the series name follows, keyed by a short line.
const tipRow = (color, name, value, sub) => h('div', { class: 'tip-row' },
  color ? h('i', { class: 'tip-key', style: { background: color } }) : h('i', { class: 'tip-key none' }),
  h('b', null, value), h('span', { class: 'tip-name' }, name), sub ? h('span', { class: 'tip-sub' }, sub) : null);

function placeTip(tip, plot, x) {
  tip.hidden = false;
  const W = plot.clientWidth;
  const left = x + 14;
  tip.style.left = `${left + tip.offsetWidth > W ? Math.max(0, x - tip.offsetWidth - 14) : left}px`;
  tip.style.top = '0px';
}

// Shared frame: header row (legend or summary + Chart/Table toggle), plot, table.
function frame({ legend, height, table }) {
  const plot = h('div', { class: 'vchart-plot', tabindex: '0', style: { height: height + 'px' } });
  const tip = h('div', { class: 'vchart-tip', hidden: true, role: 'status' });
  const tableBox = h('div', { class: 'vchart-table table-wrap', hidden: true, style: { maxHeight: `${Math.max(height, 140)}px` } });
  const toggle = table ? h('button', { class: 'chart-toggle', type: 'button', title: 'Show the values as a table' }, icon('table'), 'Table') : null;
  toggle?.addEventListener('click', () => {
    const showTable = tableBox.hidden;
    if (showTable && !tableBox.firstChild) tableBox.append(table());
    tableBox.hidden = !showTable;
    plot.hidden = showTable;
    toggle.replaceChildren(icon(showTable ? 'chart' : 'table'), showTable ? 'Chart' : 'Table');
    toggle.title = showTable ? 'Show the chart' : 'Show the values as a table';
  });
  const head = h('div', { class: 'vchart-legend' }, legend, h('span', { class: 'spacer' }), toggle);
  plot.append(tip);
  return { el: h('div', { class: 'vchart' }, head, plot, tableBox), plot, tip };
}

// Keyboard: arrows move between buckets, Enter opens the focused one.
function keyNav(plot, n, show, hide, select) {
  let cur = -1;
  plot.addEventListener('focus', () => { cur = cur < 0 ? n - 1 : cur; show(cur); });
  plot.addEventListener('blur', hide);
  plot.addEventListener('keydown', (e) => {
    if (!n) return;
    const moves = { ArrowLeft: -1, ArrowRight: 1, Home: -n, End: n };
    if (e.key in moves) {
      e.preventDefault();
      cur = Math.max(0, Math.min(n - 1, (cur < 0 ? n - 1 : cur) + moves[e.key]));
      show(cur);
    } else if (e.key === 'Enter' && select && cur >= 0) {
      select(cur);
    }
  });
  return (i) => { cur = i; };
}

function axisGrid(svg, ticks, top, gutter, W, plotH, fmt) {
  ticks.forEach((t) => {
    const y = Math.round(plotH - (t / top) * (plotH - 6)) + 0.5;
    svg.append(s('line', { class: t ? 'grid' : 'baseline', x1: gutter, x2: W, y1: y, y2: y }),
      s('text', { class: 'axis', x: gutter - 6, y: y + 3.5, 'text-anchor': 'end' }, fmt(t)));
  });
}

function xLabels(svg, n, xOf, W, gutter, height, text) {
  if (!n) return;
  const idx = W - gutter < 420 ? [0, n - 1] : [0, Math.floor(n / 3), Math.floor((2 * n) / 3), n - 1];
  [...new Set(idx)].forEach((i, k, arr) => {
    const anchor = arr.length > 1 && k === 0 ? 'start' : k === arr.length - 1 && arr.length > 1 ? 'end' : 'middle';
    svg.append(s('text', { class: 'axis', x: xOf(i, anchor), y: height - 4, 'text-anchor': anchor }, text(i, anchor)));
  });
}

// ── Stacked level volume ──
// daily: one bucket per day in the local time zone (labels show dates).
export function volumeChart(result, { series = SERIES, height = 80, onSelect, daily = false, table = true } = {}) {
  const activeSeries = series || SERIES;
  const buckets = result?.buckets || [];
  const totals = result?.totals || {};
  const stacks = buckets.map((b) => activeSeries.map((sr) => sumLevels(b.counts, sr.levels)));
  const totalOf = (i) => stacks[i].reduce((a, v) => a + v, 0);
  const peak = Math.max(0, ...stacks.map((_, i) => totalOf(i)));
  const ticks = niceTicks(peak, height >= 150 ? 4 : 2);
  const top = ticks[ticks.length - 1];
  const spanMs = ((result?.to || 0) - (result?.from || 0)) / 1e6;

  const label = (ms) => (daily ? localDate(ms).slice(5) : spanMs >= 20 * 3600e3 ? `${localDate(ms).slice(5)} ${fmtClock(ms)}` : fmtClock(ms));
  const startMs = (i) => buckets[i].start / 1e6;
  const endMs = (i) => (buckets[i].start + result.bucket_nanos) / 1e6;
  const title = (i) => (daily ? localDate(startMs(i)) : `${label(startMs(i))} to ${label(endMs(i))}`);
  const order = [...activeSeries].reverse(); // legend and tooltip read top to bottom like the stack

  const legend = [
    ...order.map((sr) => h('span', { class: 'legend-item' },
      h('i', { class: 'swatch', style: { background: sr.color } }), sr.label, h('b', null, fmtNum(sumLevels(totals, sr.levels))))),
    result?.partial ? h('span', { class: 'partial' }, 'partial: scan limit reached') : null,
  ];
  const tableView = () => h('table', { class: 'table' },
    h('thead', null, h('tr', null, h('th', null, daily ? 'Day' : 'From'), ...order.map((sr) => h('th', { class: 'num' }, sr.label)), h('th', { class: 'num' }, 'Total'), h('th', { class: 'num' }, 'Error rate'))),
    h('tbody', null, buckets.map((_, i) => {
      const t = totalOf(i);
      const errCount = t - (stacks[i][activeSeries.length - 1] || 0);
      return h('tr', null, h('td', null, title(i)),
        ...order.map((sr) => h('td', { class: 'num' }, fmtNum(stacks[i][activeSeries.indexOf(sr)]))),
        h('td', { class: 'num strong' }, fmtNum(t)), h('td', { class: 'num' }, t ? fmtPct((errCount / t) * 100) : '-'));
    }).reverse()));
  const { el, plot, tip } = frame({ legend, height, table: table && buckets.length ? tableView : null });

  if (!buckets.length) {
    plot.replaceChildren(h('div', { class: 'vchart-empty' }, 'No volume data'));
    return { el, destroy() {} };
  }

  let svg = null;
  let hl = null;
  let geo = { gutter: 0, bw: 0 };

  function draw() {
    const W = plot.clientWidth;
    if (!W) return;
    svg?.remove();
    const plotH = height - AXIS_H;
    const n = buckets.length;
    const gutter = Math.ceil(Math.max(...ticks.map((t) => fmtCompact(t).length)) * CHAR_W) + 10;
    const bw = (W - gutter) / n;
    const barW = Math.min(BAR_MAX, Math.max(1, bw - GAP));
    geo = { gutter, bw };
    svg = s('svg', { width: W, height, viewBox: `0 0 ${W} ${height}`, role: 'img', 'aria-label': `Volume, peak ${fmtNum(peak)} lines per ${daily ? 'day' : 'bucket'}` });
    axisGrid(svg, ticks, top, gutter, W, plotH, fmtCompact);
    hl = s('rect', { class: 'hl', x: gutter, y: 0, width: bw, height: plotH, rx: 3, visibility: 'hidden' });
    svg.append(hl);

    const scale = (v) => (v / top) * (plotH - 6);
    stacks.forEach((stack, i) => {
      const segs = stack.map((v, j) => ({ v, color: activeSeries[j].color })).filter((o) => o.v > 0);
      let base = plotH;
      const x = gutter + i * bw + (bw - barW) / 2;
      segs.forEach((o, k) => {
        const full = scale(o.v);
        const gap = k > 0 && full > GAP + 1 ? GAP : 0;
        const hgt = Math.max(full - gap, 1);
        const y = base - gap - hgt;
        svg.append(k === segs.length - 1
          ? s('path', { d: topRounded(x, y, barW, hgt, 4), fill: o.color })
          : s('rect', { x, y, width: barW, height: hgt, fill: o.color }));
        base = y;
      });
    });
    xLabels(svg, n, (i, anchor) => (anchor === 'start' ? gutter : anchor === 'end' ? W : gutter + i * bw + bw / 2), W, gutter, height,
      (i, anchor) => label(anchor === 'end' && !daily ? endMs(i) : startMs(i)));
    plot.insertBefore(svg, tip);
  }

  function show(i) {
    const t = totalOf(i);
    const errCount = t - (stacks[i][activeSeries.length - 1] || 0);
    hl?.setAttribute('x', geo.gutter + i * geo.bw);
    hl?.setAttribute('visibility', 'visible');
    tip.replaceChildren(
      h('div', { class: 'tip-title' }, title(i)),
      ...order.map((sr) => {
        const v = stacks[i][activeSeries.indexOf(sr)];
        return tipRow(sr.color, sr.label, fmtNum(v), t ? fmtPct((v / t) * 100) : '');
      }),
      h('div', { class: 'tip-sep' }),
      tipRow(null, 'requests in total', fmtNum(t)),
      tipRow(null, 'error rate', t ? fmtPct((errCount / t) * 100) : '-'),
      onSelect ? h('div', { class: 'tip-hint' }, daily ? 'Click to open this day' : 'Click to zoom in') : null);
    placeTip(tip, plot, geo.gutter + i * geo.bw + geo.bw);
    setCur(i);
  }
  const hide = () => {
    tip.hidden = true;
    hl?.setAttribute('visibility', 'hidden');
  };
  const select = (i) => onSelect?.(startMs(i), endMs(i));
  const bucketAt = (ev) => {
    const i = Math.floor((ev.clientX - plot.getBoundingClientRect().left - geo.gutter) / geo.bw);
    return i >= 0 && i < buckets.length ? i : -1;
  };
  const setCur = keyNav(plot, buckets.length, show, hide, onSelect ? select : null);

  plot.addEventListener('mousemove', (ev) => {
    const i = bucketAt(ev);
    if (i < 0) return hide();
    show(i);
    plot.style.cursor = onSelect ? 'pointer' : '';
  });
  plot.addEventListener('mouseleave', hide);
  plot.addEventListener('click', (ev) => {
    const i = bucketAt(ev);
    if (i >= 0) select(i);
  });

  const ro = new ResizeObserver(draw);
  ro.observe(plot);
  return { el, destroy: () => ro.disconnect() };
}

// ── Single-series line (with a 10% area wash) ──
export function lineChart(points, { height = 160, format = fmtNum, color = 'var(--accent)', name = '', summary = '', onSelect, daily = true, table = true } = {}) {
  const n = points.length;
  const peak = Math.max(0, ...points.map((p) => p.y));
  const ticks = niceTicks(peak, height >= 150 ? 4 : 2);
  const top = ticks[ticks.length - 1];
  const label = (ms) => (daily ? localDate(ms).slice(5) : fmtClock(ms));
  const tableView = () => h('table', { class: 'table' },
    h('thead', null, h('tr', null, h('th', null, daily ? 'Day' : 'Time'), h('th', { class: 'num' }, name),
      ...(points[0]?.rows || []).map(([k]) => h('th', { class: 'num' }, k)))),
    h('tbody', null, [...points].reverse().map((p) => h('tr', null, h('td', null, p.title),
      h('td', { class: 'num strong' }, format(p.y)), ...(p.rows || []).map(([, v]) => h('td', { class: 'num' }, v))))));
  const { el, plot, tip } = frame({ legend: summary ? h('span', { class: 'muted' }, summary) : null, height, table: table && n ? tableView : null });
  if (!n) {
    plot.replaceChildren(h('div', { class: 'vchart-empty' }, 'No data'));
    return { el, destroy() {} };
  }

  let svg = null;
  let cross = null;
  let dot = null;
  let geo = { gutter: 0, step: 0, xOf: () => 0, yOf: () => 0 };

  function draw() {
    const W = plot.clientWidth;
    if (!W) return;
    svg?.remove();
    const plotH = height - AXIS_H;
    const gutter = Math.ceil(Math.max(...ticks.map((t) => format(t).length)) * CHAR_W) + 10;
    const step = (W - gutter - 8) / n;
    const xOf = (i) => gutter + step * (i + 0.5);
    const yOf = (v) => plotH - (v / top) * (plotH - 6);
    geo = { gutter, step, xOf, yOf };
    svg = s('svg', { width: W, height, viewBox: `0 0 ${W} ${height}`, role: 'img', 'aria-label': `${name}, latest ${format(points[n - 1].y)}` });
    axisGrid(svg, ticks, top, gutter, W, plotH, format);
    const line = points.map((p, i) => `${i ? 'L' : 'M'}${xOf(i).toFixed(1)},${yOf(p.y).toFixed(1)}`).join('');
    svg.append(
      s('path', { d: `${line}L${xOf(n - 1).toFixed(1)},${plotH}L${xOf(0).toFixed(1)},${plotH}Z`, fill: color, 'fill-opacity': 0.1 }),
      s('path', { d: line, fill: 'none', stroke: color, 'stroke-width': 2, 'stroke-linejoin': 'round', 'stroke-linecap': 'round' }));
    cross = s('line', { class: 'crosshair', x1: 0, x2: 0, y1: 0, y2: plotH, visibility: 'hidden' });
    dot = s('circle', { r: 4, fill: color, stroke: 'var(--panel)', 'stroke-width': 2, visibility: 'hidden' });
    const last = points[n - 1];
    svg.append(cross,
      s('circle', { cx: xOf(n - 1), cy: yOf(last.y), r: 4, fill: color, stroke: 'var(--panel)', 'stroke-width': 2 }),
      s('text', { class: 'end-label', x: xOf(n - 1) - 8, y: Math.max(12, yOf(last.y) - 9), 'text-anchor': 'end' }, format(last.y)),
      dot);
    xLabels(svg, n, (i, anchor) => (anchor === 'start' ? gutter : anchor === 'end' ? W - 8 : xOf(i)), W, gutter, height, (i) => label(points[i].ms));
    plot.insertBefore(svg, tip);
  }

  function show(i) {
    const p = points[i];
    const x = geo.xOf(i);
    cross?.setAttribute('x1', x);
    cross?.setAttribute('x2', x);
    cross?.setAttribute('visibility', 'visible');
    dot?.setAttribute('cx', x);
    dot?.setAttribute('cy', geo.yOf(p.y));
    dot?.setAttribute('visibility', 'visible');
    tip.replaceChildren(h('div', { class: 'tip-title' }, p.title), tipRow(color, name, format(p.y)),
      ...(p.rows || []).map(([k, v]) => tipRow(null, k, v)),
      onSelect ? h('div', { class: 'tip-hint' }, 'Click to open') : null);
    placeTip(tip, plot, x);
    setCur(i);
  }
  const hide = () => {
    tip.hidden = true;
    cross?.setAttribute('visibility', 'hidden');
    dot?.setAttribute('visibility', 'hidden');
  };
  const indexAt = (ev) => {
    const i = Math.floor((ev.clientX - plot.getBoundingClientRect().left - geo.gutter) / geo.step);
    return Math.max(0, Math.min(n - 1, i));
  };
  const setCur = keyNav(plot, n, show, hide, onSelect ? (i) => onSelect(points[i]) : null);
  plot.addEventListener('mousemove', (ev) => {
    show(indexAt(ev));
    plot.style.cursor = onSelect ? 'pointer' : '';
  });
  plot.addEventListener('mouseleave', hide);
  plot.addEventListener('click', (ev) => onSelect?.(points[indexAt(ev)]));

  const ro = new ResizeObserver(draw);
  ro.observe(plot);
  return { el, destroy: () => ro.disconnect() };
}

// ── Sparkline for stat tiles: trend in gray, the current value in the accent ──
export function sparkline(values, { width = 104, height = 30, label = 'Trend' } = {}) {
  const n = values.length;
  if (n < 2) return h('span', { class: 'spark' });
  const max = Math.max(...values);
  const min = Math.min(0, ...values);
  const range = max - min || 1;
  const pts = values.map((v, i) => [3 + (i / (n - 1)) * (width - 8), height - 4 - ((v - min) / range) * (height - 8)]);
  const d = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('');
  const [lx, ly] = pts[n - 1];
  return h('span', { class: 'spark', title: label },
    s('svg', { width, height, viewBox: `0 0 ${width} ${height}`, role: 'img', 'aria-label': label },
      s('path', { d, fill: 'none', stroke: 'var(--spark)', 'stroke-width': 1.5, 'stroke-linejoin': 'round', 'stroke-linecap': 'round' }),
      s('circle', { cx: lx, cy: ly, r: 3.5, fill: 'var(--accent)', stroke: 'var(--panel)', 'stroke-width': 2 })));
}
