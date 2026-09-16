// The signed-in user's own account: name and password.
import { api } from '../api.js';
import { h, icon, toast, emptyState, fmtAgo } from '../ui.js';
import { store, setUser } from '../state.js';

export function mount(root) {
  let alive = true;
  const el = h('div', { class: 'page' });
  root.append(el);
  const head = h('div', { class: 'page-head' },
    h('div', null, h('h1', null, 'Profile'), h('p', null, 'Manage your account credentials and password')));

  const card = (title, sub, ...content) => h('div', { class: 'card' },
    h('div', { class: 'card-head' }, h('div', null, h('h2', null, title), h('p', { class: 'card-sub' }, sub))), ...content);
  const input = (attrs) => h('input', { class: 'input', spellcheck: 'false', ...attrs });
  const labeled = (label, control, cls = '') => h('label', { class: `field ${cls}` }, h('span', null, label), control);
  const staticField = (label, value) => h('div', { class: 'field' }, h('span', null, label), h('div', { class: 'static' }, value));
  const actions = (...btns) => h('div', { class: 'form-actions' }, ...btns);

  async function save(btn, fn) {
    btn.disabled = true;
    try { await fn(); } catch (e) { toast(e.message, 'error'); } finally { btn.disabled = false; }
  }

  async function load() {
    if (!store.authEnabled) {
      el.replaceChildren(head, emptyState('lock', 'Authentication is disabled', 'Set NINEGUARD_AUTH_ENABLED=true in your environment to enforce sign-in.'));
      return;
    }
    let data;
    try {
      data = await api.get('/profile');
    } catch (e) {
      if (alive) el.replaceChildren(head, emptyState('alert', 'Could not load profile', e.message));
      return;
    }
    if (alive) render(data.user);
  }

  function accountCard(u) {
    const name = input({ value: u.display_name || '', placeholder: u.username, autocomplete: 'name', maxlength: 64 });
    const uname = input({ value: u.username, autocomplete: 'username', required: true, disabled: true });
    const btn = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Save Changes');
    const form = h('form', null,
      h('div', { class: 'form-grid' },
        labeled('Display name', name),
        labeled('Username', uname),
        staticField('Role', u.role === 'admin' ? 'Administrator' : 'Operator'),
        staticField('Last sign-in', u.last_login_at ? fmtAgo(Date.parse(u.last_login_at)) : 'Current session')),
      actions(btn));
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      save(btn, async () => {
        const nu = await api.patch('/profile', { display_name: name.value });
        setUser(nu);
        toast('Profile saved');
      });
    });
    return card('Account Details', 'Your dashboard account information', form);
  }

  function passwordCard() {
    const cur = input({ type: 'password', autocomplete: 'current-password', required: true, placeholder: 'Enter current password' });
    const p1 = input({ type: 'password', autocomplete: 'new-password', minlength: 6, required: true, placeholder: 'Min 6 characters' });
    const p2 = input({ type: 'password', autocomplete: 'new-password', minlength: 6, required: true, placeholder: 'Re-enter new password' });
    const btn = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Update Password');
    const form = h('form', null,
      h('div', { class: 'form-grid' },
        labeled('Current password', cur, 'full'),
        labeled('New password', p1),
        labeled('Confirm new password', p2)),
      actions(btn));
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      if (p1.value !== p2.value) return toast('New passwords do not match', 'error');
      save(btn, async () => {
        await api.post('/profile/password', { current: cur.value, password: p1.value });
        form.reset();
        toast('Password updated successfully');
      });
    });
    return card('Change Password', 'Keep your administrator account secure', form);
  }

  function render(u) {
    el.replaceChildren(head,
      h('div', { class: 'grid grid-2e' }, accountCard(u), passwordCard())
    );
  }

  load();
  return { refresh: load, destroy() { alive = false; } };
}
