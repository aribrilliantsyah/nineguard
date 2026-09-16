// Shared app state: signed-in user, events, localStorage and the hash router.

const read = (k, d) => { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch { return d; } };
export const save = (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* private mode */ } };
export const load = read;

export const store = {
  version: '',
  commit: '',
  userId: '',
  user: '', // username
  displayName: '',
  role: '', // admin | operator
  authEnabled: true,
};

// setUser stores the signed-in identity ({id, username, display_name, role}).
export function setUser(u) {
  store.userId = u.id || '';
  store.user = u.username || '';
  store.displayName = u.display_name || u.username || '';
  store.role = u.role || '';
  emit('user');
}

const listeners = {};
export function on(evt, fn) {
  (listeners[evt] ||= new Set()).add(fn);
  return () => listeners[evt].delete(fn);
}
export function emit(evt, data) {
  listeners[evt]?.forEach((fn) => fn(data));
}

// ── Router: #/<view>?<params> ──
export function getRoute() {
  const raw = location.hash.replace(/^#\/?/, '');
  const [view, query = ''] = raw.split('?');
  return { view: view || 'dashboard', params: Object.fromEntries(new URLSearchParams(query)) };
}

export function href(view, params = {}) {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null)).toString();
  return `#/${view}${q ? '?' + q : ''}`;
}

export function setRoute(view, params = {}, { replace = false } = {}) {
  const hash = href(view, params);
  if (hash === location.hash) return;
  if (replace) {
    history.replaceState(null, '', hash);
    emit('route');
  } else {
    location.hash = hash;
  }
}

export function patchRoute(changes, opts) {
  const r = getRoute();
  setRoute(r.view, { ...r.params, ...changes }, opts);
}
