// DOM and formatting helpers shared by every view.

const SVG_NS = 'http://www.w3.org/2000/svg';
const PROPS = new Set(['value', 'checked', 'selected', 'disabled', 'hidden', 'innerHTML', 'innerText']);

// h('div', {class: 'x', onclick: fn}, child, [children], 'text')
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  setAttrs(el, attrs);
  appendAll(el, children);
  return el;
}

export function s(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) if (v != null) el.setAttribute(k, v);
  appendAll(el, children);
  return el;
}

function setAttrs(el, attrs) {
  if (!attrs) return;
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k === 'innerHTML') el.innerHTML = v;
    else if (k === 'innerText') el.innerText = v;
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (PROPS.has(k)) el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
}

function appendAll(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : String(c));
  }
}

export function icon(name, cls = '') {
  const svg = document.createElementNS(SVG_NS, 'svg');
  svg.setAttribute('class', ('icon ' + cls).trim());
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', '#i-' + name);
  svg.append(use);
  return svg;
}

// ── Time ──
export function fmtAgo(ms) {
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

// ── Numbers ──
export const fmtNum = (n) => (n || 0).toLocaleString('en-US');
export function fmtCompact(n) {
  n = n || 0;
  if (n < 1000) return String(n);
  if (n < 1e6) return (n / 1e3).toFixed(n < 1e4 ? 1 : 0).replace(/\.0$/, '') + 'k';
  return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
}
export function fmtBytes(b) {
  if (!b) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(b) / Math.log(1024)));
  return `${parseFloat((b / 1024 ** i).toFixed(1))} ${units[i]}`;
}

// ── Feedback ──
export function toast(msg, kind = 'ok') {
  const t = h('div', { class: `toast ${kind}` }, icon(kind === 'error' ? 'alert' : 'check'), h('span', null, msg));
  document.getElementById('toasts').append(t);
  setTimeout(() => {
    t.classList.add('out');
    setTimeout(() => t.remove(), 250);
  }, kind === 'error' ? 6000 : 3500);
}

export async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Clipboard API needs a secure context; fall back for plain-HTTP reverse proxies.
    const ta = h('textarea', { style: { position: 'fixed', opacity: '0' } });
    ta.value = text;
    document.body.append(ta);
    ta.select();
    document.execCommand('copy');
    ta.remove();
  }
  toast('Copied to clipboard');
}

export function confirmDialog({ title, body, message, confirmText = 'Confirm', danger = false, typeToConfirm = '', onConfirm }) {
  const dialogBody = body || message || '';
  return new Promise((resolve) => {
    const input = typeToConfirm ? h('input', { class: 'input', autocomplete: 'off', spellcheck: 'false' }) : null;
    const ok = h('button', { class: `btn ${danger ? 'btn-danger solid' : 'btn-primary'}`, disabled: !!typeToConfirm }, confirmText);
    const close = (v) => {
      overlay.remove();
      document.removeEventListener('keydown', onKey);
      resolve(v);
    };
    const onKey = (e) => { if (e.key === 'Escape') close(false); };
    input?.addEventListener('input', () => { ok.disabled = input.value !== typeToConfirm; });
    input?.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !ok.disabled) handleOk(); });
    const handleOk = async () => {
      ok.disabled = true;
      try {
        if (onConfirm) await onConfirm();
        close(true);
      } catch {
        ok.disabled = false;
      }
    };
    ok.addEventListener('click', handleOk);
    const overlay = h('div', { class: 'overlay', onclick: (e) => { if (e.target === overlay) close(false); } },
      h('div', { class: 'dialog', role: 'dialog', 'aria-modal': 'true' },
        h('h3', null, title),
        dialogBody ? h('p', { class: 'dialog-body' }, dialogBody) : null,
        input && h('label', { class: 'field' }, h('span', null, `Type ${typeToConfirm} to confirm`), input),
        h('div', { class: 'dialog-actions' }, h('button', { class: 'btn', type: 'button', onclick: () => close(false) }, 'Cancel'), ok)));
    document.body.append(overlay);
    document.addEventListener('keydown', onKey);
    (input || ok).focus();
  });
}

// Small dropdown anchored to a button; opens upwards when there is no room below.
export function menu(anchor, items, { up = false } = {}) {
  document.querySelectorAll('.menu').forEach((m) => m.remove());
  const r = anchor.getBoundingClientRect();
  const m = h('div', { class: 'menu', role: 'menu' },
    items.map((it) => it.label
      ? h('div', { class: 'menu-label' }, it.label)
      : h('button', { type: 'button', class: it.checked ? 'checked' : null, onclick: () => { m.remove(); it.onClick(); } }, it.icon && icon(it.icon), it.text)));
  if (up) m.style.minWidth = `${r.width}px`;
  document.body.append(m);
  const left = up ? r.left : Math.min(r.right - m.offsetWidth, window.innerWidth - m.offsetWidth - 8);
  let top = r.bottom + 6;
  if (up || top + m.offsetHeight > window.innerHeight - 8) top = Math.max(8, r.top - m.offsetHeight - 6);
  Object.assign(m.style, { top: `${top}px`, left: `${Math.max(8, left)}px` });
  setTimeout(() => {
    const off = (e) => {
      if (!m.contains(e.target)) { m.remove(); document.removeEventListener('mousedown', off); }
    };
    document.addEventListener('mousedown', off);
  });
}

export function emptyState(iconName, title, body, action) {
  return h('div', { class: 'empty' }, icon(iconName), h('h3', null, title), body && h('p', null, body), action);
}

export function skeletonRows(n = 10) {
  return Array.from({ length: n }, (_, i) => h('div', { class: 'skel skel-row', style: { width: `${55 + ((i * 37) % 40)}%` } }));
}

// Dialog with a small form. onSubmit(values) may throw to show the error and
// keep the dialog open; its result resolves the promise (false on cancel).
// fields: [{name, label, type, value, options: [[value, label]], hint, required, autocomplete, placeholder}]
export function formDialog({ title, body, fields = [], submitText = 'Save', danger = false, wide = false, cancel = true, onSubmit = async () => true }) {
  return new Promise((resolve) => {
    const inputs = {};
    const err = h('p', { class: 'form-error' });
    const ok = h('button', { class: `btn ${danger ? 'btn-danger solid' : 'btn-primary'}`, type: 'submit' }, submitText);
    const rows = fields.map((f) => {
      if (f.node) return h('div', { class: 'field' }, f.label ? h('span', null, f.label) : null, f.node);
      if (f.input) {
        if (f.name) inputs[f.name] = f.input;
        return h('label', { class: 'field' }, f.label ? h('span', null, f.label) : null, f.input, f.hint ? h('small', { class: 'muted' }, f.hint) : null);
      }
      const input = f.type === 'select'
        ? h('select', { class: 'select wide', name: f.name }, f.options.map(([v, l]) => h('option', { value: v }, l)))
        : h('input', {
          class: 'input', name: f.name, type: f.type || 'text', autocomplete: f.autocomplete || 'off', spellcheck: 'false',
          placeholder: f.placeholder || '', required: f.required !== false,
        });
      if (f.value != null) input.value = f.value;
      inputs[f.name] = input;
      return h('label', { class: 'field' }, h('span', null, f.label), input, f.hint ? h('small', { class: 'muted' }, f.hint) : null);
    });
    const close = (v) => {
      overlay.remove();
      document.removeEventListener('keydown', onKey);
      resolve(v);
    };
    const onKey = (e) => { if (e.key === 'Escape') close(false); };
    const form = h('form', { class: `dialog${wide ? ' wide' : ''}`, role: 'dialog', 'aria-modal': 'true' },
      h('h3', null, title), body ? h('p', { class: 'dialog-body' }, body) : null, rows, err,
      h('div', { class: 'dialog-actions' }, cancel ? h('button', { class: 'btn', type: 'button', onclick: () => close(false) }, 'Cancel') : null, ok));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.textContent = '';
      ok.disabled = true;
      try {
        const res = await onSubmit(Object.fromEntries(Object.entries(inputs).map(([k, el]) => [k, el.value])));
        close(res ?? true);
      } catch (e2) {
        err.textContent = e2.message;
      } finally {
        ok.disabled = false;
      }
    });
    const overlay = h('div', { class: 'overlay', onmousedown: (e) => { if (e.target === overlay) close(false); } }, form);
    document.body.append(overlay);
    document.addEventListener('keydown', onKey);
    (Object.values(inputs)[0] || ok).focus();
  });
}

// Password policy, mirrored from the server (auth.checkPassword).
const PW_RULES = [
  ['8+ characters', (p) => [...p].length >= 8],
  ['lowercase', (p) => /\p{Ll}/u.test(p)],
  ['uppercase', (p) => /\p{Lu}/u.test(p)],
  ['number', (p) => /\p{Nd}/u.test(p)],
  ['symbol', (p) => /[^\p{L}\p{Nd}\s]/u.test(p)],
];
export const PASSWORD_HINT = 'At least 8 characters with a-z, A-Z, 0-9 and a symbol, e.g. Qawsed#1477';

// Returns what a password is missing, or '' when it follows the policy.
export function passwordProblem(p) {
  if (new TextEncoder().encode(p).length > 72) return 'Password must be at most 72 characters.';
  const missing = PW_RULES.filter(([, ok]) => !ok(p)).map(([name]) => name);
  return missing.length ? `Password needs: ${missing.join(', ')} (e.g. Qawsed#1477).` : '';
}

// Checklist under a password input that ticks the rules off while typing.
export function passwordRules(input) {
  const items = PW_RULES.map(([name]) => h('li', null, icon('checkmark'), name));
  const sync = () => PW_RULES.forEach(([, ok], i) => items[i].classList.toggle('ok', ok(input.value)));
  input.addEventListener('input', sync);
  sync();
  return h('ul', { class: 'pw-rules', 'aria-label': 'Password rules' }, items);
}

// Readable random password (no look-alike characters) that follows the policy.
export function randomPassword(n = 14) {
  const sets = ['abcdefghjkmnpqrstuvwxyz', 'ABCDEFGHJKMNPQRSTUVWXYZ', '23456789', '#@$%&*!?'];
  const all = sets.join('');
  const rnd = (max) => crypto.getRandomValues(new Uint32Array(1))[0] % max;
  const out = sets.map((set) => set[rnd(set.length)]); // one of each class
  while (out.length < n) out.push(all[rnd(all.length)]);
  for (let i = out.length - 1; i > 0; i--) { // shuffle so the classes are not in a fixed order
    const j = rnd(i + 1);
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out.join('');
}

export function debounce(fn, ms) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

export function passwordField(attrs = {}) {
  const input = h('input', { class: 'input', type: 'password', ...attrs });
  const btn = h('button', {
    class: 'password-toggle',
    type: 'button',
    title: 'Show password',
    onclick: (e) => {
      e.preventDefault();
      const isPass = input.type === 'password';
      input.type = isPass ? 'text' : 'password';
      btn.title = isPass ? 'Hide password' : 'Show password';
      btn.replaceChildren(icon(isPass ? 'eye-off' : 'eye'));
    }
  }, icon('eye'));

  const wrap = h('div', { class: 'password-wrap' }, input, btn);
  return { input, wrap };
}

export { setRoute } from './state.js';
