// Time windows, query params and search highlighting for Traffic Explorer.
import { h, icon, localDate, fmtClock, zonedMs, addDays, tzLabel } from './ui.js';

export const RANGES = [
  ['day', 'Whole day'],
  ['15m', 'Last 15 min'],
  ['1h', 'Last hour'],
  ['6h', 'Last 6 hours'],
  ['24h', 'Last 24 hours'],
  ['7d', 'Last 7 days'],
  ['30d', 'Last 30 days'],
  ['custom', 'Custom'],
];
const RANGE_MS = {
  '15m': 15 * 60e3,
  '1h': 3600e3,
  '6h': 6 * 3600e3,
  '24h': 24 * 3600e3,
  '7d': 7 * 86400e3,
  '30d': 30 * 86400e3,
};

// Returns {from, to} in ms; to = null means "until now".
export function timeWindow(p) {
  if (RANGE_MS[p.range]) return { from: Date.now() - RANGE_MS[p.range], to: null };
  const at = (v) => zonedMs(v.slice(0, 10), v.slice(11, 16));
  if (p.range === 'custom' && p.from && p.to) return { from: at(p.from), to: at(p.to) + 59999 };
  const day = p.date || localDate();
  return { from: zonedMs(day), to: zonedMs(addDays(day, 1)) - 1 };
}

export const iso = (ms) => new Date(ms).toISOString();

// 'YYYY-MM-DDTHH:MM' in the local zone, for datetime-local inputs and URLs.
export function toLocalInput(ms) {
  return `${localDate(ms)}T${fmtClock(ms)}`;
}

export const shiftDate = addDays;

// Route params -> API query params.
export function queryParams(p) {
  const w = timeWindow(p);
  return {
    provider: p.provider || '',
    key: p.key || '',
    model: p.model || '',
    status: p.status || '',
    level: p.lv || '',
    search: p.q || '',
    from: iso(w.from),
    to: w.to ? iso(w.to) : '',
  };
}

const escRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// Regex sources for the free-text parts of a search, used for highlighting.
export function searchTerms(q) {
  if (!q) return [];
  const out = [];
  for (const m of q.matchAll(/"([^"]+)"|\/(.+?)\/(?=\s|$)|(\S+)/g)) {
    if (m[1]) out.push(escRe(m[1]));
    else if (m[2]) {
      try { new RegExp(m[2]); out.push(m[2]); } catch { /* invalid regex */ }
    } else if (m[3] !== 'OR' && !m[3].startsWith('-') && !/^[a-z]+:/i.test(m[3])) {
      out.push(escRe(m[3]));
    }
  }
  return out;
}

export function highlight(text, terms) {
  if (!terms || !terms.length || !text) return text;
  let re;
  try { re = new RegExp(terms.join('|'), 'gi'); } catch { return text; }
  const out = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    if (!m[0]) continue;
    out.push(text.slice(last, m.index), h('mark', null, m[0]));
    last = m.index + m[0].length;
  }
  out.push(text.slice(last));
  return out;
}

// Range select + day stepper + custom from/to inputs.
export function rangeControls(patch) {
  let cur = {};
  const sel = h('select', {
    class: 'select', 'aria-label': 'Time range',
    onchange: (e) => {
      const v = e.target.value;
      if (v === 'custom') {
        const w = timeWindow(cur);
        patch({ range: v, from: toLocalInput(w.from), to: toLocalInput(w.to ?? Date.now()) });
      } else {
        patch({ range: v === 'day' ? '' : v, from: '', to: '' });
      }
    },
  }, RANGES.map(([v, label]) => h('option', { value: v }, label)));

  const today = () => localDate();
  const date = h('input', { type: 'date', class: 'input', 'aria-label': 'Date', onchange: (e) => patch({ date: e.target.value === today() ? '' : e.target.value }) });
  const step = (n) => {
    const d = shiftDate(cur.date || today(), n);
    patch({ date: d >= today() ? '' : d });
  };
  const prev = h('button', { class: 'icon-btn sm', type: 'button', title: 'Previous day', onclick: () => step(-1) }, icon('chevron-left'));
  const next = h('button', { class: 'icon-btn sm', type: 'button', title: 'Next day', onclick: () => step(1) }, icon('chevron-right'));
  const dayGroup = h('span', { class: 'input-group' }, prev, date, next);

  const from = h('input', { type: 'datetime-local', class: 'input', 'aria-label': 'From', onchange: (e) => patch({ from: e.target.value }) });
  const to = h('input', { type: 'datetime-local', class: 'input', 'aria-label': 'To', onchange: (e) => patch({ to: e.target.value }) });
  const customGroup = h('span', { class: 'input-group' }, from, h('span', { class: 'muted' }, 'to'), to);

  return {
    el: h('span', { class: 'range', title: `Times are in ${tzLabel()}` }, icon('clock'), sel, dayGroup, customGroup, h('span', { class: 'tz' }, tzLabel())),
    sync(p) {
      cur = p;
      const range = p.range || 'day';
      sel.value = range;
      dayGroup.hidden = range !== 'day';
      customGroup.hidden = range !== 'custom';
      date.value = p.date || today();
      date.max = today();
      next.disabled = (p.date || today()) >= today();
      from.value = p.from || '';
      to.value = p.to || '';
    },
  };
}
