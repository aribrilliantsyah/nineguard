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
        setUser(nu.user || nu);
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

  const PRESET_QUESTIONS = [
    'What was the name of your first pet?',
    'In what city were you born?',
    'What was your childhood nickname?',
    'What was the make and model of your first car?',
    'What is your mother\'s maiden name?',
    'What was the name of your first elementary school?',
    'Custom question...',
  ];

  function recoveryCard(u) {
    let editing = !u.has_recovery;
    const wrap = h('div', null);

    function updateCardContent() {
      if (u.has_recovery && !editing) {
        const statusBox = h('div', { class: 'recovery-status-box configured', style: { padding: '10px 12px' } },
          icon('shield'),
          h('div', { style: { flex: '1' } },
            h('div', { class: 'strong', style: { display: 'flex', alignItems: 'center', gap: '6px' } },
              'Recovery Question Configured',
              h('span', { class: 'badge ok', style: { fontSize: '10.5px' } }, 'Active')
            ),
            h('div', { class: 'recovery-question-label' }, `Question: "${u.recovery_question || 'Active'}"`),
            h('p', { class: 'card-sub', style: { margin: '4px 0 0', fontSize: '11.5px' } }, 'You can use this security question on the login screen to recover your password if you ever lose access.')
          ),
          h('button', {
            type: 'button',
            class: 'btn btn-sm',
            style: { alignSelf: 'flex-start' },
            onclick: () => {
              editing = true;
              updateCardContent();
            }
          }, icon('pencil'), 'Change Question')
        );

        wrap.replaceChildren(statusBox);
        return;
      }

      const notice = !u.has_recovery
        ? h('div', { class: 'recovery-status-box not-configured', style: { padding: '9px 12px', marginBottom: '10px' } },
            icon('alert'),
            h('div', null,
              h('div', { class: 'strong', style: { fontSize: '13px' } }, 'No Recovery Question Set'),
              h('div', { class: 'card-sub', style: { margin: '2px 0 0', fontSize: '11.5px' } }, 'Set a recovery question and answer now so a lost password or device can be recovered safely.')
            )
          )
        : null;

      const qSelect = h('select', { class: 'input select wide' },
        ...PRESET_QUESTIONS.map(q => h('option', { value: q }, q))
      );

      const isPreset = PRESET_QUESTIONS.slice(0, -1).includes(u.recovery_question);
      if (u.recovery_question && !isPreset) {
        qSelect.value = 'Custom question...';
      } else if (u.recovery_question) {
        qSelect.value = u.recovery_question;
      }

      const customQInput = input({
        placeholder: 'Enter your custom recovery question',
        value: (!isPreset && u.recovery_question) ? u.recovery_question : '',
      });
      const customQField = labeled('Custom Question', customQInput, 'full');
      customQField.style.display = qSelect.value === 'Custom question...' ? 'flex' : 'none';

      qSelect.addEventListener('change', () => {
        customQField.style.display = qSelect.value === 'Custom question...' ? 'flex' : 'none';
        if (qSelect.value === 'Custom question...') customQInput.focus();
      });

      const ans = input({
        placeholder: 'Enter your secret answer',
        required: true,
        autocomplete: 'off',
      });

      const curPass = input({
        type: 'password',
        placeholder: 'Enter current password',
        required: true,
        autocomplete: 'current-password',
      });

      const submitBtn = h('button', { class: 'btn btn-primary', type: 'submit' },
        u.has_recovery ? 'Update Recovery Question' : 'Save Recovery Question'
      );

      const cancelBtn = u.has_recovery ? h('button', {
        type: 'button',
        class: 'btn',
        onclick: () => {
          editing = false;
          updateCardContent();
        }
      }, 'Cancel') : null;

      const form = h('form', null,
        notice,
        h('div', { class: 'form-grid' },
          labeled('Security Question', qSelect, 'full'),
          customQField,
          labeled('Your Secret Answer', ans),
          labeled('Current Password', curPass)
        ),
        actions(...[cancelBtn, submitBtn].filter(Boolean))
      );

      form.addEventListener('submit', (e) => {
        e.preventDefault();
        const chosenQ = qSelect.value === 'Custom question...' ? customQInput.value.trim() : qSelect.value.trim();
        if (!chosenQ) return toast('Please specify a recovery question', 'error');
        if (!ans.value.trim()) return toast('Please enter a recovery answer', 'error');
        if (!curPass.value) return toast('Please enter your current password', 'error');

        save(submitBtn, async () => {
          await api.post('/profile/recovery', {
            question: chosenQ,
            answer: ans.value.trim(),
            current_password: curPass.value,
          });
          u.has_recovery = true;
          u.recovery_question = chosenQ;
          setUser(u);
          toast('Recovery question configured successfully');
          editing = false;
          render(u);
        });
      });

      wrap.replaceChildren(form);
    }

    updateCardContent();
    return card('Account Recovery Question', 'Set a recovery question so a lost password or device can be recovered', wrap);
  }

  function securityOverviewCard(u) {
    const isAdm = u.role === 'admin';
    const recBadge = u.has_recovery
      ? h('span', { class: 'badge ok' }, icon('shield'), 'Configured')
      : h('span', { class: 'badge', style: { color: 'var(--muted)', background: 'var(--hover)' } }, icon('alert'), 'Not set');

    const content = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px', fontSize: '12.5px' } },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingBottom: '10px', borderBottom: '1px solid var(--border)' } },
        h('div', null,
          h('div', { class: 'strong' }, 'Recovery Status'),
          h('div', { class: 'muted', style: { fontSize: '11.5px', marginTop: '2px' } },
            u.has_recovery
              ? 'Configured for password recovery on sign-in'
              : 'Set a security question to recover your password'
          )
        ),
        recBadge
      ),
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingBottom: '10px', borderBottom: '1px solid var(--border)' } },
        h('div', null,
          h('div', { class: 'strong' }, 'Account Role'),
          h('div', { class: 'muted', style: { fontSize: '11.5px', marginTop: '2px' } },
            isAdm ? 'Administrator (full access & user management)' : 'Operator (dashboard and gateway access)'
          )
        ),
        h('span', { class: `badge${isAdm ? ' role-admin' : ''}` }, isAdm ? 'Admin' : 'Operator')
      ),
      h('div', { class: 'card-sub', style: { margin: '2px 0 0', lineHeight: '1.45', fontSize: '12px' } },
        isAdm
          ? 'As an Administrator, you can manage user accounts and reset credentials for Operator accounts in the Users menu.'
          : 'As an Operator, if you ever lose your login credentials or recovery access, an Administrator can reset your account in the Users menu.'
      )
    );

    return card('Security Overview', 'Account credentials and access policy', content);
  }

  function render(u) {
    el.replaceChildren(head,
      h('div', { class: 'grid grid-2e' },
        h('div', { class: 'grid stack' }, accountCard(u), recoveryCard(u)),
        h('div', { class: 'grid stack' }, passwordCard(), securityOverviewCard(u))
      )
    );
  }

  load();
  return { refresh: load, destroy() { alive = false; } };
}
