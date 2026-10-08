// Pure helpers for the paged API key table on Endpoints & Keys.
// No DOM access here, so the logic can be unit-tested with `node --test`.

export const KEY_LIST_DEFAULTS = Object.freeze({
  page: 1,
  limit: 25,
  sort: 'last_active',
  order: 'desc',
  q: '',
  status: 'all',
  mode: 'any',
});

export const PAGE_SIZES = [10, 25, 50, 100];
export const SORTABLE = ['name', 'status', 'created', 'last_active', 'requests', 'tokens'];
const STATUSES = ['all', 'active', 'disabled'];
const MODES = ['any', 'all', 'group', 'custom'];

const pick = (v, allowed, def) => (allowed.includes(v) ? v : def);

// Reads list state from route params (strings), falling back to defaults
// for missing or invalid values.
export function stateFromParams(params = {}) {
  const d = KEY_LIST_DEFAULTS;
  const page = parseInt(params.page, 10);
  const limit = parseInt(params.limit, 10);
  return {
    page: page >= 1 ? page : d.page,
    limit: PAGE_SIZES.includes(limit) ? limit : d.limit,
    sort: pick(params.sort, SORTABLE, d.sort),
    order: pick(params.order, ['asc', 'desc'], d.order),
    q: typeof params.q === 'string' ? params.q : d.q,
    status: pick(params.status, STATUSES, d.status),
    mode: pick(params.mode, MODES, d.mode),
  };
}

// Route params for a state: only non-default values, so URLs stay short.
export function paramsFromState(state) {
  const out = {};
  for (const [k, def] of Object.entries(KEY_LIST_DEFAULTS)) {
    if (state[k] !== def && state[k] !== '' && state[k] != null) out[k] = String(state[k]);
  }
  return out;
}

// API query for GET /api/v1/keys (always paged).
export function apiQuery(state) {
  const q = { page: state.page, limit: state.limit, sort: state.sort, order: state.order };
  if (state.q) q.q = state.q;
  if (state.status !== 'all') q.status = state.status;
  if (state.mode !== 'any') q.mode = state.mode;
  return q;
}

// Header click: a new column starts descending; the active column toggles.
export function nextSort(state, field) {
  if (state.sort !== field) return { ...state, sort: field, order: 'desc', page: 1 };
  return { ...state, order: state.order === 'desc' ? 'asc' : 'desc', page: 1 };
}

// Applies a filter/search/limit change; always returns to page 1.
export function withFilter(state, changes) {
  return { ...state, ...changes, page: 1 };
}

export const totalPages = (total, limit) => Math.max(1, Math.ceil((total || 0) / limit));

// "Showing 26–50 of 112" (en dash). Empty list -> "No keys".
export function rangeLabel(page, limit, total, shown) {
  if (!shown) return total ? `No keys on this page (${total} total)` : 'No keys';
  const from = (page - 1) * limit + 1;
  return `Showing ${from}\u2013${from + shown - 1} of ${total}`;
}

// Page buttons with ellipses: always first, last, current ±1.
// Returns numbers and the string '…'.
export function pageItems(page, pages) {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1);
  const keep = new Set([1, pages, page - 1, page, page + 1].filter((n) => n >= 1 && n <= pages));
  const sorted = [...keep].sort((a, b) => a - b);
  const out = [];
  let prev = 0;
  for (const n of sorted) {
    if (n - prev > 1) out.push('\u2026');
    out.push(n);
    prev = n;
  }
  return out;
}

// After a delete/toggle reload: an empty page beyond page 1 steps back one page.
export function pageAfterReload(page, shown) {
  return shown === 0 && page > 1 ? page - 1 : page;
}
