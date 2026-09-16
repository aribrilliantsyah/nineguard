// App shell: router, grouped sidebar, command palette (Ctrl+K), topbar.
import { api, redirectToLogin } from './api.js';
import { h, icon, menu, emptyState, toast } from './ui.js';
import { store, on, save, load, getRoute, setRoute, setUser } from './state.js';
import { openPalette } from './palette.js';
import * as dashboard from './views/dashboard.js';
import * as providers from './views/providers.js';
import * as endpoints from './views/endpoints.js';
import * as reports from './views/reports.js';
import * as models from './views/models.js';
import * as traffic from './views/traffic.js';
import * as logs from './views/logs.js';
import * as profile from './views/profile.js';
import * as users from './views/users.js';
import * as about from './views/about.js';

const APP = 'NineGuard';
const VIEWS = { dashboard, providers, endpoints, reports, models, traffic, logs, profile, users, about };

// Sidebar menu, top to bottom. Groups without a title render as plain links.
// auth: only with authentication enabled; admin: only for administrators.
const NAV = [
  { id: 'main', links: [{ view: 'dashboard', label: 'Dashboard', icon: 'overview', keywords: 'home overview' }] },
  { id: 'gateway', title: 'Gateway', links: [
    { view: 'providers', label: 'Providers', icon: 'server', keywords: 'providers upstream openai compatible route apikey target prefix' },
    { view: 'endpoints', label: 'Endpoints & Keys', icon: 'key', keywords: 'endpoints agent setup url integration cursor cline continue python node curl' },
    { view: 'models', label: 'Models', icon: 'box', keywords: 'models enable disable firewall' },
    { view: 'traffic', label: 'Traffic Explorer', icon: 'clock', keywords: 'traffic requests telemetry tokens latency usage' },
    { view: 'logs', label: 'Log Explorer', icon: 'logs', keywords: 'logs server system gateway stdout error debug kibana elk' },
    { view: 'reports', label: 'Usage Reports', icon: 'activity', keywords: 'reports usage breakdown tokens consumers keys models attribution analytics' },
  ]},
  {
    id: 'account', title: 'Account', auth: true, links: [
      { view: 'profile', label: 'Profile', icon: 'user', keywords: 'account password 2fa qr recovery' },
      { view: 'users', label: 'Users', icon: 'users', keywords: 'accounts roles operators admin', admin: true },
    ],
  },
  { id: 'about', links: [{ view: 'about', label: 'About', icon: 'info', keywords: 'author credits stack license version help' }] },
];

const $ = (id) => document.getElementById(id);
const rootEl = document.documentElement;
const viewRoot = $('view');
const collapsed = new Set(load('nineguard_groups', []));
const isAdmin = () => store.authEnabled && store.role === 'admin';
const visible = (item) => (!item.auth || store.authEnabled) && (!item.admin || isAdmin());
const pages = () => NAV.filter(visible).flatMap((g) => g.links.filter(visible));
let current = null;

// ── Routing ──
function renderRoute() {
  const r = getRoute();
  const page = pages().find((l) => l.view === r.view);
  if (!VIEWS[r.view] || !page) return setRoute('dashboard', {}, { replace: true });
  if (current?.view !== r.view) {
    current?.inst.destroy?.();
    viewRoot.replaceChildren();
    viewRoot.scrollTop = 0;
    current = { view: r.view, inst: VIEWS[r.view].mount(viewRoot) };
  }
  current.inst.update?.(r.params);
  document.title = `${page.label} - ${APP}`;
  rootEl.classList.remove('sidebar-open');

  const crumbs = [h('b', null, r.params.live ? 'Live tail' : page.label)];
  if (r.view === 'traffic') {
    for (const v of [r.params.key, r.params.model, r.params.provider]) {
      if (v) crumbs.push(icon('chevron-right'), h('span', null, v));
    }
  } else if (r.view === 'logs') {
    for (const v of [r.params.source]) {
      if (v) crumbs.push(icon('chevron-right'), h('span', null, v));
    }
  }
  $('crumbs').replaceChildren(...crumbs);
  renderSidebar(r);
}

// ── Sidebar ──
function sideLink(to, label, ic, active, extra = []) {
  return h('a', { class: `side-link${active ? ' active' : ''}`, href: to, title: label }, icon(ic), h('span', { class: 'name' }, label), ...extra);
}

function sideGroup(id, title, links) {
  const el = h('section', { class: `side-group${collapsed.has(id) ? ' collapsed' : ''}` });
  if (title) {
    el.append(h('button', {
      class: 'side-group-title', type: 'button',
      onclick: () => {
        if (collapsed.has(id)) collapsed.delete(id);
        else collapsed.add(id);
        save('nineguard_groups', [...collapsed]);
        el.classList.toggle('collapsed');
      },
    }, title, icon('chevron-down')));
  }
  el.append(h('div', { class: 'side-links' }, links));
  return el;
}

function renderSidebar(r = getRoute()) {
  $('side-nav').replaceChildren(...NAV.filter(visible).map((g) => {
    const links = g.links.filter(visible).map((l) => sideLink(`#/${l.view}`, l.label, l.icon, r.view === l.view));
    return links.length ? sideGroup(g.id, g.title || '', links) : null;
  }).filter(Boolean));
}

// ── Command palette: pages from NAV, then actions ──
function paletteItems() {
  const items = pages().map((l) => ({ group: 'Pages', label: l.label, icon: l.icon, keywords: l.keywords || '', run: () => setRoute(l.view) }));
  items.push(
    { group: 'Actions', label: 'Toggle dark mode', icon: rootEl.dataset.theme === 'dark' ? 'sun' : 'moon', keywords: 'theme light', run: toggleTheme },
    { group: 'Actions', label: 'Refresh data', icon: 'refresh', keywords: 'reload', run: refreshAll },
  );
  if (store.authEnabled) items.push({ group: 'Actions', label: 'Sign out', icon: 'logout', keywords: 'logout', run: signOut });
  return items;
}

const showPalette = () => openPalette(paletteItems);
$('palette').addEventListener('click', showPalette);
$('search-kbd').textContent = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl K';

// Ctrl+K opens the menu; "/" jumps to the search box of the current page.
document.addEventListener('keydown', (e) => {
  const typing = /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || '');
  if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault();
    showPalette();
  } else if (e.key === '/' && !typing) {
    e.preventDefault();
    const box = document.querySelector('[data-log-search]') || document.querySelector('[data-search]');
    if (box) { box.focus(); box.select(); } else showPalette();
  }
});

// ── Topbar ──
function toggleTheme() {
  const t = rootEl.dataset.theme === 'dark' ? 'light' : 'dark';
  rootEl.dataset.theme = t;
  try { localStorage.setItem('nineguard_theme', t); } catch { /* private mode */ }
}

function refreshAll() {
  current?.inst.refresh?.();
}

$('toggle-sidebar').addEventListener('click', () => {
  if (matchMedia('(max-width: 860px)').matches) {
    rootEl.classList.toggle('sidebar-open');
    return;
  }
  save('nineguard_sidebar', rootEl.classList.toggle('sidebar-collapsed') ? 'collapsed' : '');
});
document.querySelector('.main').addEventListener('click', (e) => {
  if (!e.target.closest('#toggle-sidebar')) rootEl.classList.remove('sidebar-open');
});
$('theme').addEventListener('click', toggleTheme);
$('refresh').addEventListener('click', refreshAll);

async function signOut() {
  try { await api.post('/auth/logout'); } catch { /* already gone */ }
  location.href = '/login';
}

function renderAvatar() {
  $('avatar').textContent = (store.displayName || store.user || '?').charAt(0).toUpperCase();
  $('avatar').title = store.authEnabled ? `${store.displayName} (${store.role})` : 'Account';
}

$('avatar').addEventListener('click', () => menu($('avatar'), store.authEnabled
  ? [
    { label: `${store.displayName} · ${store.role === 'admin' ? 'Administrator' : 'Operator'}` },
    { icon: 'user', text: 'Profile', onClick: () => setRoute('profile') },
    ...(isAdmin() ? [{ icon: 'users', text: 'Users', onClick: () => setRoute('users') }] : []),
    { icon: 'info', text: 'About', onClick: () => setRoute('about') },
    { icon: 'logout', text: 'Sign out', onClick: signOut },
  ]
  : [{ label: 'Authentication is disabled' }, { icon: 'info', text: 'About', onClick: () => setRoute('about') }]));

// ── Start ──
async function start() {
  let st;
  try {
    st = await api.get('/auth/status');
  } catch (e) {
    viewRoot.replaceChildren(emptyState('alert', `Cannot reach ${APP}`, e.message,
      h('button', { class: 'btn', onclick: () => location.reload() }, 'Retry')));
    return;
  }
  if (st.auth_enabled && !st.authenticated) {
    if (st.setup_needed) {
      location.href = '/setup';
    } else {
      redirectToLogin();
    }
    return;
  }
  store.authEnabled = st.auth_enabled;
  setUser(st.user || { id: '', username: st.username, display_name: st.username, role: 'admin' });
  renderAvatar();

  const cfg = await api.get('/config').catch(() => null);
  if (cfg) {
    store.version = cfg.version;
    store.commit = cfg.commit;
    $('brand-ver').textContent = [...new Set([cfg.version, cfg.commit].filter(Boolean))].join(' · ');
  }

  on('user', () => { renderAvatar(); renderSidebar(); });
  on('route', renderRoute);
  window.addEventListener('hashchange', renderRoute);
  renderRoute();

  // An administrator without a recovery question can only be rescued by another admin.
  if (isAdmin()) {
    api.get('/profile').then((p) => {
      if (p.user) setUser(p.user);
      if (!p.user.has_recovery) {
        toast('Set a recovery question in your profile, so a lost password or phone can be recovered', 'error', {
          text: 'Set Up Now',
          onClick: () => setRoute('profile'),
        });
      }
    }).catch(() => {});
  }
}

start();
