// User management (administrators): accounts, roles, and password resets. No 2FA.
import { api } from '../api.js';
import { h, icon, fmtAgo, toast, menu, confirmDialog, formDialog, emptyState } from '../ui.js';
import { store, setUser } from '../state.js';

const ROLES = [['operator', 'Operator: access dashboard, models, and traffic'], ['admin', 'Administrator: also manages users']];
const roleBadge = (r) => h('span', { class: `badge${r === 'admin' ? ' role-admin' : ''}` }, r === 'admin' ? 'Admin' : 'Operator');

const recoveryBadge = (u) => u.has_recovery
  ? h('span', {
      class: 'badge ok',
      title: u.recovery_question ? `Question: "${u.recovery_question}"` : 'Recovery question configured',
      style: { cursor: 'help' },
    }, icon('shield'), 'Configured')
  : h('span', {
      class: 'badge',
      title: 'No recovery question configured for this account',
      style: { color: 'var(--muted)', background: 'var(--hover)' },
    }, icon('alert'), 'Not set');

export function mount(root) {
  let alive = true;
  const box = h('div', { class: 'card table-card' });
  const addBtn = store.role === 'admin'
    ? h('button', { class: 'btn btn-primary', type: 'button', onclick: addUser }, icon('plus'), 'Add User')
    : null;

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null, h('h1', null, 'Users'), h('p', null, 'Manage user access to the NineGuard dashboard')),
      h('div', { class: 'page-actions' }, addBtn)),
    box
  ));

  async function load() {
    if (store.role !== 'admin') {
      if (alive) box.replaceChildren(emptyState('lock', 'Access Denied', 'Only administrators can manage users.'));
      return;
    }
    let list;
    try {
      list = await api.get('/users');
    } catch (e) {
      if (alive) box.replaceChildren(emptyState('alert', 'Could not load users', e.message));
      return;
    }
    if (alive) render(list || []);
  }

  function render(list) {
    box.replaceChildren(h('div', { class: 'table-wrap' }, h('table', { class: 'table' },
      h('thead', null, h('tr', null,
        h('th', null, 'User'),
        h('th', null, 'Role'),
        h('th', null, 'Recovery Question'),
        h('th', null, 'Last sign-in'),
        h('th', null, 'Created'),
        h('th'))),
      h('tbody', null, list.map((u) => {
        const self = u.id === store.userId;
        return h('tr', null,
          h('td', null,
            h('div', { class: 'strong' }, u.display_name || u.username, self ? h('span', { class: 'badge', style: { marginLeft: '6px' } }, 'you') : null),
            h('div', { class: 'sub' }, u.username)),
          h('td', null, roleBadge(u.role)),
          h('td', null, recoveryBadge(u)),
          h('td', { class: 'muted' }, u.last_login_at ? fmtAgo(Date.parse(u.last_login_at)) : 'Never'),
          h('td', { class: 'muted' }, (u.created_at || '').slice(0, 10)),
          h('td', { class: 'num' }, actions(u, self)));
      })))));
  }

  function actions(u, self) {
    const items = [];
    if (self) {
      items.push({ icon: 'user', text: 'Open my profile', onClick: () => { location.hash = '#/profile'; } });
    } else {
      // Non-admin roles (operator) can be reset by administrators
      if (u.role !== 'admin') {
        items.push({
          icon: 'key',
          text: 'Reset password',
          onClick: () => resetPassword(u),
        });
        if (u.has_recovery) {
          items.push({
            icon: 'refresh',
            text: 'Reset recovery question',
            onClick: () => resetRecovery(u),
          });
        }
      }
      items.push({
        icon: 'trash',
        text: 'Delete user',
        onClick: () => remove(u),
      });
    }

    const b = h('button', {
      class: 'icon-btn sm', type: 'button', title: 'Actions',
      onclick: () => menu(b, items),
    }, icon('more'));
    return b;
  }

  async function resetPassword(u) {
    const res = await formDialog({
      title: `Reset Password: ${u.username}`,
      submitText: 'Reset password',
      fields: [
        {
          name: 'password',
          label: 'New password',
          type: 'password',
          placeholder: 'Min 6 characters',
          autocomplete: 'new-password',
        },
      ],
      onSubmit: async (v) => {
        if (!v.password || v.password.length < 6) {
          throw new Error('Password must be at least 6 characters');
        }
        await api.post(`/users/${u.id}/reset-password`, { password: v.password });
      },
    });
    if (!res) return;
    toast(`Password for ${u.username} has been reset`);
    load();
  }

  async function resetRecovery(u) {
    const ok = await confirmDialog({
      title: `Reset recovery question for ${u.username}?`,
      body: `This will clear the recovery question for "${u.username}". The user will need to configure a new recovery question in their Profile.`,
      confirmText: 'Reset recovery',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.post(`/users/${u.id}/reset-recovery`);
      toast(`Recovery question for ${u.username} has been reset`);
      load();
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function addUser() {
    const res = await formDialog({
      title: 'Add User', wide: true, submitText: 'Create user',
      fields: [
        { name: 'username', label: 'Username', placeholder: 'e.g. operator1', autocomplete: 'off' },
        { name: 'display_name', label: 'Display name', required: false, placeholder: 'Optional' },
        { name: 'role', label: 'Role', type: 'select', options: ROLES, value: 'operator' },
        { name: 'password', label: 'Password', type: 'password', placeholder: 'Min 6 characters' },
      ],
      onSubmit: async (v) => {
        if (!v.username || !v.password) throw new Error('Username and password required');
        const user = await api.post('/users', v);
        return { user };
      },
    });
    if (!res) return;
    toast(`User ${res.user.username} created`);
    load();
  }

  async function remove(u) {
    const ok = await confirmDialog({
      title: `Delete ${u.username}?`, body: 'This account will be permanently removed.',
      confirmText: 'Delete user', danger: true, typeToConfirm: u.username,
    });
    if (!ok) return;
    try {
      await api.del(`/users/${u.id}`);
      toast(`User ${u.username} deleted`);
    } catch (e) {
      toast(e.message, 'error');
    }
    load();
  }

  load();
  return { refresh: load, destroy() { alive = false; } };
}
