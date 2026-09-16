// Log Explorer: Kibana-style system, server and gateway logs discovery.
import { api } from '../api.js';
import { h, icon, fmtTime, fmtNum, localDate, tzLabel, msOf, podColor, copy, toast, menu, emptyState, skeletonRows, debounce, searchableSelect } from '../ui.js';
import { store, patchRoute, LEVELS } from '../state.js';
import { queryParams, rangeControls, searchTerms, highlight, toLocalInput, iso } from '../filters.js';
import { volumeChart } from '../chart.js';

const PAGE = 500;
const MAX_ROWS = 5000;

const keyOf = (x) => JSON.stringify([x.source, x.lv, x.q, x.range, x.date, x.from, x.to]);

export function mount(root) {
  let p = {};
  let entries = [];
  let cursor = '';
  let result = null;
  let reqId = 0;
  let loadingMore = false;
  let liveTimer = null;
  let liveState = '';
  let chart = null;
  let terms = [];
  let availableSources = [];

  const patch = (c) => patchRoute(c);

  // Range controls with date stepper and custom time pickers
  const range = rangeControls((c) => patch(c.range === 'custom' || c.from || c.date ? { ...c, live: '' } : c));

  const sourceSel = searchableSelect({
    placeholder: 'All sources',
    searchPlaceholder: 'Search log sources...',
    ariaLabel: 'Log Source',
    clearable: true,
    compact: true,
    onChange: (val) => patch({ source: val }),
  });

  const clearBtn = h('button', {
    class: 'btn btn-sm', type: 'button',
    onclick: () => patch({ source: '', lv: '', q: '' }),
  }, icon('x'), 'Clear');

  const searchIn = h('input', {
    type: 'search', autocomplete: 'off', spellcheck: 'false', 'aria-label': 'Search logs', 'data-log-search': '',
    placeholder: 'Search logs  (press /)',
    title: 'Words are ANDed. "exact phrase", -exclude, or field filters source: level:',
  });

  const pushQuery = debounce(() => patch({ q: searchIn.value.trim() }), 400);
  searchIn.addEventListener('input', pushQuery);
  searchIn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') patch({ q: searchIn.value.trim() });
    else if (e.key === 'Escape') searchIn.blur();
  });

  const searchBox = h('label', { class: 'tool-search' }, icon('search'), searchIn);

  const chips = LEVELS.map((l) => h('button', {
    class: `chip lv-${l}`, type: 'button', title: `Show or hide ${l}`,
    onclick: () => toggleLevel(l)
  }, h('i', { class: 'dot' }), l));

  const liveBtn = h('button', { class: 'chip live', type: 'button', onclick: toggleLive });

  const exportBtn = h('button', {
    class: 'btn btn-sm', type: 'button',
    onclick: () => menu(exportBtn, [
      { label: 'Export matching system logs' },
      { icon: 'download', text: 'CSV', onClick: () => doExport('csv') },
      { icon: 'download', text: 'JSON', onClick: () => doExport('json') },
    ]),
  }, icon('download'), 'Export');

  const chartBox = h('div', { class: 'histogram' });
  const body = h('div', { class: 'log-body' });
  const status = h('div', { class: 'statusbar' });

  root.append(h('div', { class: 'page-fill' },
    h('div', { class: 'toolbar' },
      searchBox, range.el, h('span', { class: 'sep' }), sourceSel, clearBtn,
      h('span', { class: 'spacer' }), h('span', { class: 'chips' }, chips), liveBtn, exportBtn),
    chartBox,
    h('div', { class: 'log-table' },
      h('div', { class: 'log-head' },
        h('span', { title: 'Local time zone' }, `Time (${tzLabel()})`),
        h('span', null, 'Level'),
        h('span', { class: 'c-src' }, 'Source'),
        h('span', null, 'Message')),
      body),
    status));

  body.addEventListener('scroll', () => {
    if (cursor && !p.live && body.scrollTop + body.clientHeight > body.scrollHeight - 300) loadMore();
  }, { passive: true });

  // ── Filters ──
  const levelSet = () => new Set(p.lv ? p.lv.split(',') : LEVELS);
  function toggleLevel(l) {
    const set = levelSet();
    if (set.has(l)) set.delete(l);
    else set.add(l);
    if (!set.size) return;
    patch({ lv: set.size === LEVELS.length ? '' : LEVELS.filter((x) => set.has(x)).join(',') });
  }

  function toggleLive() {
    if (p.live) return patch({ live: '' });
    const pastDay = (!p.range || p.range === 'day') && p.date && p.date !== localDate();
    patch({ live: '1', ...(pastDay || p.range === 'custom' ? { range: '15m', date: '', from: '', to: '' } : {}) });
  }

  function fill(sel, all, options, value) {
    if (sel.setOptions) {
      sel.setOptions(options, value, all);
      return;
    }
    const opts = value && !options.includes(value) ? [value, ...options] : options;
    sel.replaceChildren(h('option', { value: '' }, all), ...opts.map((o) => h('option', { value: o }, o)));
    sel.value = value || '';
    sel.classList.toggle('has-value', !!value);
  }

  function syncControls() {
    range.sync(p);
    if (document.activeElement !== searchIn) searchIn.value = p.q || '';

    fill(sourceSel, 'All sources', availableSources, p.source);

    const set = levelSet();
    chips.forEach((c, i) => c.classList.toggle('active', set.has(LEVELS[i])));
    clearBtn.hidden = !(p.source || p.lv || p.q);

    liveBtn.classList.toggle('active', !!p.live);
    liveBtn.title = p.live ? 'Stop following new logs' : 'Follow new logs in real time';
    liveBtn.replaceChildren(icon(p.live ? 'pause' : 'play'), 'Live',
      ...(p.live ? [h('i', { class: `live-dot${liveState === 'open' ? '' : ' wait'}`, title: liveState === 'open' ? 'Live active' : 'Connecting' })] : []));
  }

  async function loadSources() {
    try {
      const res = await api.get('/logs/sources');
      if (res && res.sources) {
        availableSources = res.sources;
        syncControls();
      }
    } catch { /* ignore */ }
  }

  // ── Loading ──
  async function reload() {
    const id = ++reqId;
    entries = [];
    cursor = '';
    result = null;
    terms = searchTerms(p.q);
    body.replaceChildren(...skeletonRows(14));
    chart?.destroy();
    chart = null;
    chartBox.replaceChildren(h('div', { class: 'skel', style: { height: '94px' } }));
    renderStatus(true);

    const q = { ...queryParams(p), source: p.source || '' };
    const [logs, vol] = await Promise.allSettled([
      api.get('/logs', { ...q, limit: PAGE }),
      api.get('/logs/volume', { ...q, to: q.to || iso(Date.now()), buckets: 60 }),
    ]);

    if (id !== reqId) return;

    if (logs.status === 'fulfilled') {
      result = logs.value;
      entries = result.entries || result.logs || [];
      cursor = result.next_cursor || '';
      renderRows();
    } else {
      body.replaceChildren(emptyState('alert', 'Could not load system logs', logs.reason.message));
    }
    renderChart(vol);
    renderStatus();
  }

  async function loadMore() {
    if (!cursor || loadingMore) return;
    loadingMore = true;
    renderStatus();
    const id = reqId;
    try {
      const q = { ...queryParams(p), source: p.source || '', limit: PAGE, cursor };
      const res = await api.get('/logs', q);
      if (id !== reqId) return;
      const fresh = res.entries || res.logs || [];
      cursor = res.next_cursor || '';
      result = res;
      if (!entries.length) body.replaceChildren();
      entries = entries.concat(fresh);
      const frag = document.createDocumentFragment();
      fresh.forEach((e) => frag.append(rowEl(e)));
      body.append(frag);
      if (!entries.length) body.replaceChildren(emptyView());
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      loadingMore = false;
      renderStatus();
    }
  }

  // ── Live tail ──
  function startLive() {
    if (liveTimer) return;
    liveState = 'open';
    syncControls();
    liveTimer = setInterval(pollFresh, 2000);
  }

  function stopLive() {
    if (liveTimer) clearInterval(liveTimer);
    liveTimer = null;
    liveState = '';
  }

  async function pollFresh() {
    if (!result || !p.live) return;
    try {
      const q = { ...queryParams(p), source: p.source || '', limit: 50 };
      const res = await api.get('/logs', q);
      const fresh = res.entries || res.logs || [];
      if (!fresh.length) return;

      const newestId = entries.length ? entries[0].id : 0;
      const unseens = fresh.filter((e) => e.id > newestId);
      if (unseens.length) {
        addFresh(unseens.reverse());
      }
    } catch { /* ignore glitches */ }
  }

  function addFresh(list) {
    if (!result || !p.live) return;
    if (!entries.length) body.replaceChildren();
    const before = body.scrollHeight;
    const top = body.scrollTop;
    const frag = document.createDocumentFragment();
    list.forEach((e) => {
      const r = rowEl(e);
      r.classList.add('fresh');
      frag.append(r);
    });
    body.prepend(frag);
    entries = list.concat(entries);
    if (entries.length > MAX_ROWS) {
      const excess = entries.length - MAX_ROWS;
      entries.length = MAX_ROWS;
      for (let i = 0; i < excess; i++) body.lastElementChild?.remove();
    }
    if (top > 0) body.scrollTop = top + (body.scrollHeight - before);
    renderStatus();
  }

  async function doExport(format) {
    try {
      toast(`Preparing ${format.toUpperCase()} export...`);
      await api.download('/logs/export', { ...queryParams(p), source: p.source || '', format });
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  // ── Rendering ──
  function renderChart(vol) {
    if (vol.status !== 'fulfilled') {
      chartBox.replaceChildren(h('div', { class: 'vchart-empty error-text' }, vol.reason.message));
      return;
    }
    chart = volumeChart(vol.value, {
      height: 74,
      onSelect: (from, to) => patch({ range: 'custom', date: '', live: '', from: toLocalInput(from), to: toLocalInput(Math.max(to - 1, from)) }),
    });
    chartBox.replaceChildren(chart.el);
  }

  function renderRows() {
    if (!entries.length) {
      body.replaceChildren(emptyView());
      return;
    }
    const frag = document.createDocumentFragment();
    entries.forEach((e) => frag.append(rowEl(e)));
    body.replaceChildren(frag);
    body.scrollTop = 0;
  }

  function emptyView() {
    if (p.live) return emptyState('activity', 'Waiting for new log lines', 'Nothing matched yet. New lines appear here as soon as they are written.');
    return emptyState('inbox', 'No logs match', 'Nothing in this time range matches the current filters.',
      h('span', { class: 'input-group' },
        h('button', { class: 'btn btn-sm', onclick: () => patch({ range: '24h', date: '', from: '', to: '' }) }, 'Last 24 hours'),
        h('button', { class: 'btn btn-sm', onclick: () => patch({ range: '', date: '', from: '', to: '', source: '', lv: '', q: '' }) }, 'Reset filters')));
  }

  function rowEl(e) {
    const ms = msOf(e);
    const source = e.source || 'server';
    const row = h('div', { class: `row lv-${e.level || 'INFO'}`, onclick: () => toggleDetail(row, e) },
      h('span', { class: 'c-time', title: e.timestamp }, fmtTime(ms)),
      h('span', null, h('span', { class: 'lv' }, e.level || 'INFO')),
      h('span', { class: 'c-src', title: source },
        h('i', { class: 'pod-dot', style: { background: podColor(source) } }),
        h('span', { class: 'src-wl' }, source)),
      h('span', { class: 'c-msg' }, highlight(e.message || '', terms)));
    return row;
  }

  function toggleDetail(row, e) {
    if (getSelection()?.toString()) return;
    const open = row.nextElementSibling?.classList.contains('detail');
    body.querySelectorAll('.detail').forEach((d) => {
      d.previousElementSibling?.classList.remove('open');
      d.remove();
    });
    if (open) return;
    row.classList.add('open');
    row.after(detailEl(e));
  }

  function detailEl(e) {
    const ms = msOf(e);
    const meta = [
      ['Timestamp', e.timestamp ? new Date(e.timestamp).toLocaleString() : '-'],
      ['Level', e.level || 'INFO'],
      ['Source', e.source || 'server'],
    ];

    let attrsObj = null;
    if (e.attrs) {
      try { attrsObj = JSON.parse(e.attrs); } catch { /* ignore */ }
    }
    if (attrsObj && typeof attrsObj === 'object') {
      for (const [k, v] of Object.entries(attrsObj)) {
        meta.push([k, typeof v === 'object' ? JSON.stringify(v) : String(v)]);
      }
    }

    const action = (ic, text, fn) => h('button', { class: 'btn btn-sm', type: 'button', onclick: fn }, icon(ic), text);

    return h('div', { class: 'detail' },
      h('div', { class: 'detail-meta' }, meta.map(([k, v]) => h('div', null, h('span', { class: 'k' }, k), h('span', { class: 'v', title: v }, v || '-')))),
      h('pre', { class: 'detail-msg' }, highlight(e.message || '', terms)),
      h('div', { class: 'detail-actions' },
        action('copy', 'Copy message', () => copy(e.message || '')),
        action('copy', 'Copy JSON', () => copy(JSON.stringify(e, null, 2))),
        e.source ? action('server', 'Only this source', () => patch({ source: e.source })) : null,
        action('crosshair', 'Surrounding lines', () => patch({
          source: '', q: '', lv: '', live: '', date: '',
          range: 'custom', from: toLocalInput(ms - 60e3), to: toLocalInput(ms + 60e3),
        }))));
  }

  function renderStatus(loading) {
    const liveText = liveState === 'open' ? 'Live: streaming' : '';
    status.replaceChildren(...[
      h('span', null, loading ? 'Loading...' : `${fmtNum(entries.length)} lines, newest first`),
      h('span', null, `${availableSources.length} sources`),
      p.live && liveText ? h('span', { class: 'warn' }, icon('activity'), liveText) : null,
      h('span', { class: 'spacer' }),
      cursor && !loading ? h('button', { class: 'btn btn-sm', type: 'button', disabled: loadingMore, onclick: loadMore }, loadingMore ? 'Loading...' : 'Load older') : null,
    ].filter(Boolean));
  }

  loadSources();

  return {
    update(params) {
      const prev = keyOf(p);
      p = params;
      syncControls();
      const changed = keyOf(p) !== prev || !result;
      if (!p.live) stopLive();
      if (changed) {
        stopLive();
        const loading = reload();
        if (p.live) loading.then(startLive);
      } else if (p.live) {
        startLive();
      }
    },
    refresh: () => {
      loadSources();
      reload().then(() => { if (p.live) { stopLive(); startLive(); } });
    },
    destroy() {
      stopLive();
      chart?.destroy();
    },
  };
}
