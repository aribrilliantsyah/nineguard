// Setup (first run) and Sign in. No 2FA: simple username and hashed password.
import { h, icon } from './ui.js';

const APP = 'NineGuard';
const wrap = document.getElementById('card-wrap');
const card = document.getElementById('card');
const params = new URLSearchParams(location.search);

document.getElementById('copyright').textContent = `© ${new Date().getFullYear()} ${APP}`;
document.getElementById('theme').addEventListener('click', () => {
  const t = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = t;
  try { localStorage.setItem('nineguard_theme', t); } catch {}
});

function nextURL() {
  const n = params.get('next') || '/';
  const path = n.startsWith('/') && !n.startsWith('//') ? n : '/';
  return path + location.hash;
}

async function post(path, body) {
  let resp;
  try {
    resp = await fetch('/api/v1' + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    });
  } catch {
    throw new Error(`Cannot reach the ${APP} server`);
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || `Request failed (HTTP ${resp.status})`);
  return data;
}

function field(label, attrs) {
  const input = h('input', { class: 'input', required: true, spellcheck: 'false', ...attrs });
  return { input, el: h('label', { class: 'field' }, h('span', null, label), input) };
}

const title = (text, sub) => h('div', { class: 'auth-title' }, h('h1', null, text), h('p', null, sub));
const submit = (text) => h('button', { class: 'btn btn-primary btn-block', type: 'submit' }, text);

async function busy(btn, text, fn) {
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = text;
  try { await fn(); } finally { btn.disabled = false; btn.textContent = old; }
}

// ── Initial Setup (First Run) ──
function renderSetup() {
  wrap.classList.remove('wide');
  const user = field('Admin Username', { value: 'admin', autocomplete: 'username' });
  const pass = field('Password', { type: 'password', minlength: 6, autocomplete: 'new-password', placeholder: 'Enter a secure password' });
  const pass2 = field('Confirm Password', { type: 'password', minlength: 6, autocomplete: 'new-password', placeholder: 'Re-enter password' });
  const err = h('p', { class: 'form-error' });
  const btn = submit('Initialize Administrator Account');

  const form = h('form', { class: 'auth-form' }, user.el, pass.el, pass2.el, btn, err);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    err.textContent = '';
    if (pass.input.value !== pass2.input.value) {
      err.textContent = 'Passwords do not match.';
      return;
    }
    busy(btn, 'Setting up...', async () => {
      try {
        await post('/auth/setup', {
          username: user.input.value.trim(),
          password: pass.input.value,
        });
        location.replace(nextURL());
      } catch (err2) {
        err.textContent = err2.message;
      }
    });
  });

  card.replaceChildren(
    title('Welcome to NineGuard', 'Create your administrator credentials to secure the dashboard'),
    form
  );
  pass.input.focus();
}

// ── Standard Sign In ──
function renderLogin() {
  wrap.classList.remove('wide');
  const user = field('Username', { autocomplete: 'username', value: 'admin' });
  const pass = field('Password', { type: 'password', autocomplete: 'current-password' });
  const err = h('p', { class: 'form-error' });
  const btn = submit('Sign in');

  const forgotWrap = h('div', { style: { display: 'flex', justifyContent: 'flex-end', margin: '-4px 0 8px' } },
    h('a', {
      href: '#recover',
      style: { fontSize: '11.5px', color: 'var(--accent)', textDecoration: 'none', cursor: 'pointer' },
      onclick: (e) => {
        e.preventDefault();
        renderRecovery(user.input.value.trim());
      }
    }, 'Forgot password?')
  );

  const form = h('form', { class: 'auth-form' }, user.el, pass.el, forgotWrap, btn, err);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    err.textContent = '';
    busy(btn, 'Signing in...', async () => {
      try {
        await post('/auth/login', {
          username: user.input.value.trim(),
          password: pass.input.value,
        });
        location.replace(nextURL());
      } catch (err2) {
        err.textContent = err2.message;
        pass.input.select();
      }
    });
  });

  card.replaceChildren(
    title('Sign in', `Welcome back to ${APP}`),
    form,
    h('p', { class: 'auth-hint' }, icon('lock'), ' Session stays signed in for 30 days.')
  );
  pass.input.focus();
}

// ── Password Recovery Flow ──
function renderRecovery(initialUsername = 'admin') {
  wrap.classList.remove('wide');
  const user = field('Username', { autocomplete: 'username', value: initialUsername || 'admin' });
  const err = h('p', { class: 'form-error' });
  const step1Btn = submit('Continue');
  const backLink = h('p', { class: 'auth-hint', style: { marginTop: '12px' } },
    h('a', {
      href: '#login',
      style: { color: 'var(--accent)', textDecoration: 'none', cursor: 'pointer', fontSize: '12px' },
      onclick: (e) => { e.preventDefault(); renderLogin(); }
    }, '← Back to sign in')
  );

  const form = h('form', { class: 'auth-form' }, user.el, step1Btn, err);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    err.textContent = '';
    const username = user.input.value.trim();
    if (!username) {
      err.textContent = 'Please enter your username.';
      return;
    }
    busy(step1Btn, 'Checking...', async () => {
      let data;
      try {
        const resp = await fetch('/api/v1/auth/recovery?username=' + encodeURIComponent(username));
        data = await resp.json().catch(() => ({}));
        if (!resp.ok) throw new Error(data.error || 'Failed to check recovery status');
      } catch (err2) {
        err.textContent = err2.message;
        return;
      }
      renderRecoveryStep2(username, data.question);
    });
  });

  card.replaceChildren(
    title('Recover Password', 'Answer your recovery question to reset your password'),
    form,
    backLink
  );
  user.input.focus();
}

function renderRecoveryStep2(username, question) {
  const qBox = h('div', { class: 'card', style: { padding: '12px 14px', marginBottom: '12px', background: 'var(--panel-2)' } },
    h('div', { style: { fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--muted)', marginBottom: '4px' } }, 'Recovery Question'),
    h('div', { class: 'strong', style: { fontSize: '13px', color: 'var(--text)' } }, question)
  );

  const ans = field('Your Secret Answer', { autocomplete: 'off', placeholder: 'Enter answer' });
  const p1 = field('New Password', { type: 'password', minlength: 6, autocomplete: 'new-password', placeholder: 'Min 6 characters' });
  const p2 = field('Confirm New Password', { type: 'password', minlength: 6, autocomplete: 'new-password', placeholder: 'Re-enter new password' });
  const err = h('p', { class: 'form-error' });
  const btn = submit('Reset Password');
  const backLink = h('p', { class: 'auth-hint', style: { marginTop: '12px' } },
    h('a', {
      href: '#login',
      style: { color: 'var(--accent)', textDecoration: 'none', cursor: 'pointer', fontSize: '12px' },
      onclick: (e) => { e.preventDefault(); renderLogin(); }
    }, '← Back to sign in')
  );

  const form = h('form', { class: 'auth-form' }, qBox, ans.el, p1.el, p2.el, btn, err);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    err.textContent = '';
    if (p1.input.value !== p2.input.value) {
      err.textContent = 'New passwords do not match.';
      return;
    }
    busy(btn, 'Resetting...', async () => {
      try {
        await post('/auth/recovery', {
          username,
          answer: ans.input.value.trim(),
          new_password: p1.input.value,
        });
        card.replaceChildren(
          title('Password Reset', 'Your password has been changed successfully'),
          h('div', { style: { display: 'flex', flexDirection: 'column', gap: '14px', marginTop: '14px' } },
            h('p', { class: 'auth-hint' }, icon('check'), ' Sign in using your new password.'),
            h('button', {
              class: 'btn btn-primary btn-block',
              type: 'button',
              onclick: () => renderLogin()
            }, 'Sign in now')
          )
        );
      } catch (err2) {
        err.textContent = err2.message;
      }
    });
  });

  card.replaceChildren(
    title('Security Question', `Resetting password for ${username}`),
    form,
    backLink
  );
  ans.input.focus();
}

// Pre-render immediately based on pathname so there is zero skeleton delay
if (location.pathname === '/setup') {
  document.title = `Setup - ${APP}`;
  renderSetup();
} else {
  document.title = `Sign in - ${APP}`;
  renderLogin();
}

async function start() {
  let st;
  try {
    st = await (await fetch('/api/v1/auth/status')).json();
  } catch {
    card.replaceChildren(title(`${APP} is unreachable`, 'Check that the server is running, then reload this page.'));
    return;
  }
  if (!st.auth_enabled || st.authenticated) return location.replace(nextURL());
  if (st.setup_needed) {
    if (location.pathname !== '/setup') {
      history.replaceState(null, '', '/setup' + location.hash);
      document.title = `Setup - ${APP}`;
      renderSetup();
    }
  } else {
    if (location.pathname === '/setup') {
      history.replaceState(null, '', '/login' + location.hash);
      document.title = `Sign in - ${APP}`;
      renderLogin();
    }
  }
}

start();
