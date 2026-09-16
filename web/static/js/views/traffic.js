// Traffic Explorer: Kibana/Discover-style HTTP request, latency and token telemetry.
import { api } from '../api.js';
import { h, icon, fmtTime, fmtNum, fmtCompact, localDate, tzLabel, msOf, podColor, copy, toast, menu, emptyState, skeletonRows, debounce, searchableSelect } from '../ui.js';
import { patchRoute } from '../state.js';
import { queryParams, rangeControls, searchTerms, highlight, toLocalInput, iso } from '../filters.js';
import { volumeChart } from '../chart.js';

const PAGE = 500;
const MAX_ROWS = 5000;

const TRAFFIC_STATUSES = [
  { id: '2xx', label: '2xx OK', cls: 'status-chip-2xx' },
  { id: '403', label: '403 Blocked', cls: 'status-chip-403' },
  { id: '4xx', label: '4xx Error', cls: 'status-chip-4xx' },
  { id: '5xx', label: '5xx Error', cls: 'status-chip-5xx' },
];

const TRAFFIC_SERIES = [
  { label: '5xx', levels: ['5xx'], color: 'var(--danger)' },
  { label: '4xx', levels: ['4xx'], color: 'var(--lv-warn)' },
  { label: '403', levels: ['403'], color: '#c42b1c' },
  { label: '2xx', levels: ['2xx'], color: 'var(--ok)' },
];

const keyOf = (x) => JSON.stringify([x.key, x.model, x.provider, x.ip, x.status, x.q, x.range, x.date, x.from, x.to]);

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

  let availableKeys = [];
  let availableModels = [];
  let availableProviders = [];

  const patch = (c) => patchRoute(c);

  // Range controls with stepper and custom time inputs
  const range = rangeControls((c) => patch(c.range === 'custom' || c.from || c.date ? { ...c, live: '' } : c));

  const select = (label, key, clears, searchPlaceholder) => searchableSelect({
    placeholder: label,
    searchPlaceholder,
    ariaLabel: label,
    clearable: true,
    compact: true,
    onChange: (val) => patch({ [key]: val, ...Object.fromEntries(clears.map((k) => [k, ''])) }),
  });

  const keySel = select('All client keys', 'key', [], 'Search client keys...');
  const modelSel = select('All models', 'model', [], 'Search models...');
  const providerSel = select('All providers', 'provider', [], 'Search providers...');

  const clearBtn = h('button', {
    class: 'btn btn-sm', type: 'button',
    onclick: () => patch({ key: '', model: '', provider: '', ip: '', status: '', q: '' }),
  }, icon('x'), 'Clear');

  const searchIn = h('input', {
    type: 'search', autocomplete: 'off', spellcheck: 'false', 'aria-label': 'Search traffic logs', 'data-log-search': '',
    placeholder: 'Search traffic logs  (press /)',
    title: 'Words are ANDed. "exact phrase", -exclude, or field filters key: model: provider: ip: status:',
  });

  const pushQuery = debounce(() => patch({ q: searchIn.value.trim() }), 400);
  searchIn.addEventListener('input', pushQuery);
  searchIn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') patch({ q: searchIn.value.trim() });
    else if (e.key === 'Escape') searchIn.blur();
  });

  const searchBox = h('label', { class: 'tool-search' }, icon('search'), searchIn);

  // Status Filter Chips (2xx, 403, 4xx, 5xx)
  const chips = TRAFFIC_STATUSES.map((s) => h('button', {
    class: `chip ${s.cls}`, type: 'button', title: `Filter ${s.label}`,
    onclick: () => toggleStatus(s.id)
  }, h('i', { class: 'dot' }), s.label));

  const liveBtn = h('button', { class: 'chip live', type: 'button', onclick: toggleLive });

  const exportBtn = h('button', {
    class: 'btn btn-sm', type: 'button',
    onclick: () => menu(exportBtn, [
      { label: 'Export matching traffic logs' },
      { icon: 'download', text: 'CSV', onClick: () => doExport('csv') },
      { icon: 'download', text: 'JSON', onClick: () => doExport('json') },
    ]),
  }, icon('download'), 'Export');

  const chartBox = h('div', { class: 'histogram' });
  const body = h('div', { class: 'log-body' });
  const status = h('div', { class: 'statusbar' });

  root.append(h('div', { class: 'page-fill' },
    h('div', { class: 'toolbar' },
      searchBox, range.el, h('span', { class: 'sep' }), keySel, modelSel, providerSel, clearBtn,
      h('span', { class: 'spacer' }), h('span', { class: 'chips' }, chips), liveBtn, exportBtn),
    chartBox,
    h('div', { class: 'log-table traffic-table' },
      h('div', { class: 'log-head' },
        h('span', { title: `Local time zone (${tzLabel()})` }, 'Time'),
        h('span', null, 'Guard Status'),
        h('span', null, 'Client Key'),
        h('span', null, 'Target Model'),
        h('span', { class: 'num' }, 'Tokens'),
        h('span', { class: 'num' }, 'Latency'),
        h('span', null, 'Client IP')),
      body),
    status));

  body.addEventListener('scroll', () => {
    if (cursor && !p.live && body.scrollTop + body.clientHeight > body.scrollHeight - 300) loadMore();
  }, { passive: true });

  // ── Status Filters ──
  const statusSet = () => new Set(p.status ? p.status.split(',') : TRAFFIC_STATUSES.map((s) => s.id));
  function toggleStatus(id) {
    const set = statusSet();
    if (set.has(id)) set.delete(id);
    else set.add(id);
    if (!set.size) return;
    patch({ status: set.size === TRAFFIC_STATUSES.length ? '' : TRAFFIC_STATUSES.map((s) => s.id).filter((x) => set.has(x)).join(',') });
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

  function getModelOptions() {
    const map = new Map();
    availableModels.forEach((m) => {
      const id = typeof m === 'string' ? m : (m.id || m.name);
      if (id) {
        let badge = (typeof m === 'object' && m.provider_id) ? m.provider_id : '';
        if (!badge && id.includes('/')) badge = id.split('/')[0];
        map.set(id, { value: id, label: id, badge });
      }
    });
    entries.forEach((e) => {
      if (e.model && !map.has(e.model)) {
        const badge = e.provider_id || (e.model.includes('/') ? e.model.split('/')[0] : '');
        map.set(e.model, { value: e.model, label: e.model, badge });
      }
    });
    return Array.from(map.values()).sort((a, b) => a.label.localeCompare(b.label));
  }

  function getKeyOptions() {
    const map = new Map();
    availableKeys.forEach((k) => {
      const name = typeof k === 'string' ? k : (k.name || k.key);
      const val = typeof k === 'string' ? k : (k.name || k.key);
      if (name) {
        const label = (typeof k === 'object' && k.name && k.key) ? `${k.name} (${k.key})` : name;
        map.set(val, { value: val, label, badge: (typeof k === 'object' && k.role) ? k.role : 'key' });
      }
    });
    entries.forEach((e) => {
      const id = e.api_key_name || e.api_key;
      if (id && !map.has(id)) {
        const label = (e.api_key_name && e.api_key) ? `${e.api_key_name} (${e.api_key})` : id;
        map.set(id, { value: id, label, badge: 'traffic' });
      }
    });
    return Array.from(map.values()).sort((a, b) => a.label.localeCompare(b.label));
  }

  function getProviderOptions() {
    const map = new Map();
    availableProviders.forEach((pr) => {
      const id = typeof pr === 'string' ? pr : (pr.id || pr.prefix || pr.name);
      if (id) {
        const label = (typeof pr === 'object' && pr.name) ? `${pr.name} (${pr.prefix || id})` : id;
        const badge = (typeof pr === 'object' && pr.prefix) ? pr.prefix : 'provider';
        map.set(id, { value: id, label, badge });
      }
    });
    entries.forEach((e) => {
      if (e.provider_id && !map.has(e.provider_id)) {
        map.set(e.provider_id, { value: e.provider_id, label: e.provider_id, badge: 'traffic' });
      }
    });
    return Array.from(map.values()).sort((a, b) => a.label.localeCompare(b.label));
  }

  function syncControls() {
    range.sync(p);
    if (document.activeElement !== searchIn) searchIn.value = p.q || '';

    fill(keySel, 'All client keys', getKeyOptions(), p.key);
    fill(modelSel, 'All models', getModelOptions(), p.model);
    fill(providerSel, 'All providers', getProviderOptions(), p.provider);

    const set = statusSet();
    chips.forEach((c, i) => c.classList.toggle('active', set.has(TRAFFIC_STATUSES[i].id)));
    clearBtn.hidden = !(p.key || p.model || p.provider || p.ip || p.status || p.q);

    liveBtn.classList.toggle('active', !!p.live);
    liveBtn.title = p.live ? 'Stop following new requests' : 'Follow new requests in real time';
    liveBtn.replaceChildren(icon(p.live ? 'pause' : 'play'), 'Live',
      ...(p.live ? [h('i', { class: `live-dot${liveState === 'open' ? '' : ' wait'}`, title: liveState === 'open' ? 'Live active' : 'Connecting' })] : []));
  }

  async function loadCatalogs() {
    try {
      const [keysRes, modelsRes, providersRes] = await Promise.allSettled([
        api.get('/keys'),
        api.get('/models'),
        api.get('/providers'),
      ]);
      if (keysRes.status === 'fulfilled') {
        availableKeys = Array.isArray(keysRes.value) ? keysRes.value : (keysRes.value?.keys || []);
      }
      if (modelsRes.status === 'fulfilled') {
        availableModels = Array.isArray(modelsRes.value) ? modelsRes.value : (modelsRes.value?.models || []);
      }
      if (providersRes.status === 'fulfilled') {
        availableProviders = Array.isArray(providersRes.value) ? providersRes.value : (providersRes.value?.providers || []);
      }
      syncControls();
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

    const q = queryParams(p);
    const [logs, vol] = await Promise.allSettled([
      api.get('/traffic', { ...q, limit: PAGE }),
      api.get('/traffic/volume', { ...q, to: q.to || iso(Date.now()), buckets: 60 }),
    ]);

    if (id !== reqId) return;

    if (logs.status === 'fulfilled') {
      result = logs.value;
      entries = result.entries || result.logs || [];
      cursor = result.next_cursor || '';
      renderRows();
    } else {
      body.replaceChildren(emptyState('alert', 'Could not load traffic logs', logs.reason.message));
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
      const res = await api.get('/traffic', { ...queryParams(p), limit: PAGE, cursor });
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
    liveTimer = setInterval(pollFresh, 2500);
  }

  function stopLive() {
    if (liveTimer) clearInterval(liveTimer);
    liveTimer = null;
    liveState = '';
  }

  async function pollFresh() {
    if (!result || !p.live) return;
    try {
      const q = { ...queryParams(p), limit: 50 };
      const res = await api.get('/traffic', q);
      const fresh = res.entries || res.logs || [];
      if (!fresh.length) return;

      const newestId = entries.length ? entries[0].id : 0;
      const unseens = fresh.filter((e) => e.id > newestId);
      if (unseens.length) {
        addFresh(unseens.reverse());
      }
    } catch { /* ignore network glitches */ }
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
    syncControls();
    renderStatus();
  }

  async function doExport(format) {
    try {
      toast(`Preparing ${format.toUpperCase()} export...`);
      await api.download('/traffic/export', { ...queryParams(p), format });
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
      series: TRAFFIC_SERIES,
      height: 74,
      onSelect: (from, to) => patch({ range: 'custom', date: '', live: '', from: toLocalInput(from), to: toLocalInput(Math.max(to - 1, from)) }),
    });
    chartBox.replaceChildren(chart.el);
  }

  function renderRows() {
    syncControls();
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
    if (p.live) return emptyState('activity', 'Waiting for new requests', 'Nothing matched yet. New requests to NineGuard appear here in real time.');
    return emptyState('inbox', 'No traffic recorded yet', 'Nothing in this time range matches the current filters.',
      h('span', { class: 'input-group' },
        h('button', { class: 'btn btn-sm', onclick: () => patch({ range: '24h', date: '', from: '', to: '' }) }, 'Last 24 hours'),
        h('button', { class: 'btn btn-sm', onclick: () => patch({ range: '', date: '', from: '', to: '', key: '', model: '', provider: '', ip: '', status: '', q: '' }) }, 'Reset filters')));
  }

  function renderStatusBadge(code) {
    const isOk = code >= 200 && code < 400;
    const isBlk = code === 403;
    const statusClass = isOk ? 's-2xx' : (isBlk ? 's-403' : (code < 500 ? 's-warn' : 's-err'));
    const statusText = isBlk ? '403 Blocked' : (isOk ? `${code} OK` : `${code} Error`);
    const statusIcon = isBlk ? icon('shield') : (isOk ? icon('checkmark') : icon('alert'));

    return h('span', { class: `badge-status ${statusClass}` },
      statusIcon,
      statusText
    );
  }

  function rowEl(e) {
    const ms = msOf(e);
    const isErr = e.status_code === 403 || e.status_code >= 500;
    const isWarn = e.status_code >= 400 && !isErr;
    const rowCls = e.status_code === 403 ? 's-row-403' : (isErr ? 'lv-ERROR' : (isWarn ? 'lv-WARN' : ''));

    // Time: formatted e.g. "09:59:58 AM" matching dashboard
    const dateObj = e.timestamp ? new Date(e.timestamp) : (ms ? new Date(ms) : null);
    const timeStr = dateObj ? dateObj.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '-';

    // Model prefix and short name
    const slashIdx = (e.model || '').lastIndexOf('/');
    const modShort = slashIdx >= 0 ? e.model.slice(slashIdx + 1) : (e.model || '-');
    const modPrefix = slashIdx >= 0 ? e.model.slice(0, slashIdx + 1) : '';

    // Latency and Stream badge
    const latStr = e.duration_ms > 0 ? `${fmtNum(e.duration_ms)}ms` : '< 1ms';
    const streamBadge = e.stream ? h('span', { class: 'badge', style: { fontSize: '9.5px', padding: '1px 4px', marginLeft: '4px' } }, 'SSE') : null;

    const row = h('div', { class: `row ${rowCls}`, onclick: () => toggleDetail(row, e) },
      // 1. Time
      h('span', { class: 'c-time', title: e.timestamp ? new Date(e.timestamp).toLocaleString() : timeStr }, timeStr),
      // 2. Guard Status
      h('span', null, renderStatusBadge(e.status_code)),
      // 3. Client Key
      h('span', { class: 'c-key', title: `${e.api_key_name || 'Client Key'}${e.api_key ? ` (${e.api_key})` : ''}` },
        h('b', null, highlight(e.api_key_name || 'Client Key', terms)),
        e.api_key ? h('span', { class: 'muted' }, highlight(e.api_key, terms)) : null
      ),
      // 4. Target Model
      h('span', { class: 'c-model', title: `${e.model || '-'}${e.error_message ? `\nError: ${e.error_message}` : ''}` },
        modPrefix ? h('span', { class: 'source-ns' }, modPrefix) : null,
        h('span', { class: 'strong' }, highlight(modShort, terms)),
        e.error_message ? h('span', { class: 'traffic-err-msg' }, '(', highlight(e.error_message, terms), ')') : null
      ),
      // 5. Tokens
      h('span', { class: 'num c-tokens' },
        h('span', { title: `Prompt: ${fmtNum(e.prompt_tokens || 0)} · Comp: ${fmtNum(e.completion_tokens || 0)}` },
          fmtCompact(e.total_tokens || 0)
        )
      ),
      // 6. Latency
      h('span', { class: 'num c-lat' },
        latStr,
        streamBadge
      ),
      // 7. Client IP
      h('span', { class: 'c-ip muted', title: e.client_ip || '' },
        highlight(e.client_ip || '-', terms)
      )
    );
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

  function formatDuration(ms) {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  }

  function detailEl(e) {
    const ms = msOf(e);
    let statusText = `${e.status_code}`;
    if (e.status_code === 200) statusText = '200 OK';
    else if (e.status_code === 403) statusText = '403 Forbidden (Blocked by NineGuard Firewall)';
    else if (e.status_code === 400) statusText = '400 Bad Request';
    else if (e.status_code === 401) statusText = '401 Unauthorized';
    else if (e.status_code === 404) statusText = '404 Not Found';
    else if (e.status_code === 429) statusText = '429 Too Many Requests (Rate Limited)';
    else if (e.status_code === 500) statusText = '500 Internal Server Error';
    else if (e.status_code === 502) statusText = '502 Bad Gateway (Upstream Provider Error)';
    else if (e.status_code === 503) statusText = '503 Service Unavailable';
    else if (e.status_code === 504) statusText = '504 Gateway Timeout';
    else if (e.status_code >= 200 && e.status_code < 300) statusText = `${e.status_code} OK`;
    else if (e.status_code >= 400 && e.status_code < 500) statusText = `${e.status_code} Client Error`;
    else if (e.status_code >= 500) statusText = `${e.status_code} Server Error`;

    const meta = [
      ['Timestamp', e.timestamp ? new Date(e.timestamp).toLocaleString() : '-'],
      ['Guard Status', statusText],
      ['Client Key', e.api_key_name ? `${e.api_key_name} (${e.api_key})` : (e.api_key || '-')],
      ['Target Model', e.model || '-'],
      ['Provider', e.provider_id || '(default upstream)'],
      ['Tokens', `${fmtNum(e.total_tokens)} (prompt: ${fmtNum(e.prompt_tokens)}, completion: ${fmtNum(e.completion_tokens)})`],
      ['Latency', formatDuration(e.duration_ms)],
      ['Mode', e.stream ? 'Server-Sent Events (SSE)' : 'Synchronous JSON'],
      ['Client IP', e.client_ip || '-'],
    ];

    const action = (ic, text, fn) => h('button', { class: 'btn btn-sm', type: 'button', onclick: fn }, icon(ic), text);

    return h('div', { class: 'detail' },
      h('div', { class: 'detail-meta' }, meta.map(([k, v]) => h('div', null, h('span', { class: 'k' }, k), h('span', { class: 'v', title: v }, v || '-')))),
      e.error_message ? h('div', { class: 'note warn', style: { marginBottom: '10px' } }, icon('alert'), h('b', null, 'Error: '), e.error_message) : null,
      h('pre', { class: 'detail-msg' }, highlight(e.message || JSON.stringify(e, null, 2), terms)),
      h('div', { class: 'detail-actions' },
        action('copy', 'Copy message', () => copy(e.message || '')),
        action('copy', 'Copy JSON', () => copy(JSON.stringify(e, null, 2))),
        e.api_key_name || e.api_key ? action('key', 'Filter key', () => patch({ key: e.api_key_name || e.api_key })) : null,
        e.model ? action('box', 'Filter model', () => patch({ model: e.model })) : null,
        e.provider_id ? action('server', 'Filter provider', () => patch({ provider: e.provider_id })) : null,
        e.status_code ? action('shield', `Filter status ${e.status_code}`, () => patch({ status: `${e.status_code}` })) : null,
        e.client_ip ? action('search', 'Filter IP', () => patch({ q: `ip:${e.client_ip}` })) : null,
        action('crosshair', 'Surrounding requests', () => patch({
          key: '', model: '', provider: '', ip: '', status: '', q: '', live: '', date: '',
          range: 'custom', from: toLocalInput(ms - 60e3), to: toLocalInput(ms + 60e3),
        }))));
  }

  function renderStatus(loading) {
    const liveText = liveState === 'open' ? 'Live: streaming' : '';
    status.replaceChildren(...[
      h('span', null, loading ? 'Loading...' : `${fmtNum(entries.length)} requests, newest first`),
      h('span', null, `${availableProviders.length} providers`),
      p.live && liveText ? h('span', { class: 'warn' }, icon('activity'), liveText) : null,
      h('span', { class: 'spacer' }),
      cursor && !loading ? h('button', { class: 'btn btn-sm', type: 'button', disabled: loadingMore, onclick: loadMore }, loadingMore ? 'Loading...' : 'Load older') : null,
    ].filter(Boolean));
  }

  loadCatalogs();

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
      loadCatalogs();
      reload().then(() => { if (p.live) { stopLive(); startLive(); } });
    },
    destroy() {
      stopLive();
      chart?.destroy();
    },
  };
}
