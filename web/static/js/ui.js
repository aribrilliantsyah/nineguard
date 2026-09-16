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
export function nanoOf(e) {
  if (!e) return 0n;
  const ts = e.timestamp || '';
  const m = /^(.*?)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)$/.exec(ts);
  if (!m) return BigInt(Date.parse(ts) || 0) * 1000000n;
  return BigInt(Date.parse(m[1] + m[3])) * 1000000n + BigInt((m[2] || '').padEnd(9, '0').slice(0, 9));
}
export const msOf = (e) => {
  if (typeof e === 'number') return e;
  if (!e) return Date.now();
  if (e.timestamp) {
    const d = Date.parse(e.timestamp);
    if (!isNaN(d)) return d;
  }
  return Number(nanoOf(e) / 1000000n);
};

let TZ = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
let partsFmt = null;
export function setTimeZone(tz) {
  if (!tz) return;
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: tz });
    TZ = tz;
    partsFmt = null;
  } catch { /* unknown zone: keep the browser's */ }
}
export const timeZone = () => TZ;
export function tzLabel() {
  try {
    return new Intl.DateTimeFormat('id-ID', { timeZone: TZ, timeZoneName: 'short' })
      .formatToParts(Date.now()).find((p) => p.type === 'timeZoneName')?.value || TZ;
  } catch {
    return TZ;
  }
}
function parts(ms) {
  partsFmt ||= new Intl.DateTimeFormat('en-US', {
    timeZone: TZ, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
  const o = {};
  for (const p of partsFmt.formatToParts(ms)) o[p.type] = p.value;
  return o;
}
export function zonedMs(date, time = '00:00') {
  const [y, mo, d] = date.split('-').map(Number);
  const [hh, mi] = time.split(':').map(Number);
  const wall = Date.UTC(y, mo - 1, d, hh || 0, mi || 0);
  const offset = (ms) => {
    const p = parts(ms);
    return Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute, +p.second) - Math.floor(ms / 1000) * 1000;
  };
  const first = wall - offset(wall);
  return wall - offset(first);
}
export const addDays = (date, n) => new Date(Date.parse(date + 'T12:00:00Z') + n * 86400e3).toISOString().slice(0, 10);
const pad = (n, w = 2) => String(n).padStart(w, '0');

export function localDate(d = Date.now()) {
  const p = parts(d instanceof Date ? d.getTime() : typeof d === 'string' ? Date.parse(d) : d);
  return `${p.year}-${p.month}-${p.day}`;
}
export function fmtTime(ms) {
  const p = parts(ms);
  return `${p.hour}:${p.minute}:${p.second}.${pad(((ms % 1000) + 1000) % 1000, 3)}`;
}
export function fmtClock(ms) {
  const p = parts(ms);
  return `${p.hour}:${p.minute}`;
}
export function fmtDateTime(ms) {
  const p = parts(ms);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`;
}

export function fmtAgo(ms) {
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

// ── Identity Colors ──
export function podColor(name) {
  if (!name) return 'hsl(200 45% 52%)';
  let x = 0;
  for (let i = 0; i < name.length; i++) x = (x * 31 + name.charCodeAt(i)) >>> 0;
  return `hsl(${x % 360} 45% 52%)`;
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
export function toast(msg, kind = 'ok', action = null) {
  const content = [icon(kind === 'error' ? 'alert' : (kind === 'warn' ? 'alert' : 'check'))];
  if (typeof msg === 'string') {
    content.push(h('span', { class: 'toast-text' }, msg));
  } else if (msg instanceof Node) {
    content.push(msg);
  }
  let t;
  if (action && action.text) {
    const actBtn = h('button', {
      type: 'button',
      class: 'btn btn-xs btn-primary toast-action',
      onclick: (e) => {
        e.stopPropagation();
        t?.remove();
        action.onClick?.();
      }
    }, action.text);
    content.push(actBtn);
  }
  t = h('div', { class: `toast ${kind}${action ? ' has-action' : ''}` }, ...content);
  document.getElementById('toasts')?.append(t);
  setTimeout(() => {
    t.classList.add('out');
    setTimeout(() => t.remove(), 250);
  }, action ? 9000 : (kind === 'error' ? 6000 : 3500));
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
export function formDialog({ title, body, fields = [], submitText = 'Save', danger = false, wide = false, extraWide = false, className = '', cancel = true, onSubmit = async () => true }) {
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
      let input;
      if (f.type === 'search-select' || (f.type === 'select' && (f.searchable || (f.options && f.options.length > 8)))) {
        input = searchableSelect({
          placeholder: f.placeholder || 'Select...',
          searchPlaceholder: f.searchPlaceholder || `Search ${f.label || ''}...`.trim(),
          options: (f.options || []).map((opt) => (Array.isArray(opt) ? { value: opt[0], label: opt[1] } : opt)),
          value: f.value ?? '',
          wide: true,
          compact: false,
          ariaLabel: f.label || '',
        });
      } else if (f.type === 'select') {
        input = h('select', { class: 'select wide', name: f.name }, f.options.map(([v, l]) => h('option', { value: v }, l)));
        if (f.value != null) input.value = f.value;
      } else {
        input = h('input', {
          class: 'input', name: f.name, type: f.type || 'text', autocomplete: f.autocomplete || 'off', spellcheck: 'false',
          placeholder: f.placeholder || '', required: f.required !== false,
        });
        if (f.value != null) input.value = f.value;
      }
      inputs[f.name] = input;
      return h('label', { class: 'field' }, h('span', null, f.label), input, f.hint ? h('small', { class: 'muted' }, f.hint) : null);
    });
    const close = (v) => {
      overlay.remove();
      document.removeEventListener('keydown', onKey);
      resolve(v);
    };
    const onKey = (e) => { if (e.key === 'Escape') close(false); };
    const formClasses = ['dialog'];
    if (wide) formClasses.push('wide');
    if (extraWide) formClasses.push('extra-wide');
    if (className) formClasses.push(className);
    const form = h('form', { class: formClasses.join(' '), role: 'dialog', 'aria-modal': 'true' },
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

// ── Searchable Select (Combobox) ──
function highlightMatches(text, terms) {
  if (!terms || !terms.length || !text) return [document.createTextNode(text)];
  const validTerms = terms.map((t) => t.trim()).filter(Boolean);
  if (!validTerms.length) return [document.createTextNode(text)];

  const escaped = validTerms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  const re = new RegExp('(' + escaped.join('|') + ')', 'gi');
  const fragments = [];
  let lastIdx = 0;
  for (const m of text.matchAll(re)) {
    if (m.index > lastIdx) {
      fragments.push(document.createTextNode(text.slice(lastIdx, m.index)));
    }
    const mark = document.createElement('mark');
    mark.className = 'hl';
    mark.textContent = m[0];
    fragments.push(mark);
    lastIdx = m.index + m[0].length;
  }
  if (lastIdx < text.length) {
    fragments.push(document.createTextNode(text.slice(lastIdx)));
  }
  return fragments.length ? fragments : [document.createTextNode(text)];
}

function positionPopover(trigger, popover, minWidth = 220, maxWidth = 380) {
  const r = trigger.getBoundingClientRect();
  const pad = 8;
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  let width = Math.max(r.width, minWidth);
  if (width > maxWidth && r.width <= maxWidth) width = maxWidth;
  if (width > vw - pad * 2) width = vw - pad * 2;
  popover.style.width = `${Math.round(width)}px`;

  let left = r.left;
  if (left + width > vw - pad) {
    left = vw - pad - width;
  }
  if (left < pad) left = pad;

  const spaceBelow = vh - r.bottom - pad;
  const spaceAbove = r.top - pad;

  let top;
  const listEl = popover.querySelector('.search-select-list');

  if (spaceBelow < 200 && spaceAbove > spaceBelow) {
    const avail = Math.min(280, Math.max(120, spaceAbove - 46));
    if (listEl) listEl.style.maxHeight = `${avail}px`;
    const h = popover.offsetHeight;
    top = Math.max(pad, r.top - h - 4);
    popover.classList.add('open-up');
    popover.classList.remove('open-down');
  } else {
    const avail = Math.min(280, Math.max(120, spaceBelow - 46));
    if (listEl) listEl.style.maxHeight = `${avail}px`;
    top = r.bottom + 4;
    popover.classList.add('open-down');
    popover.classList.remove('open-up');
  }

  popover.style.left = `${Math.round(left)}px`;
  popover.style.top = `${Math.round(top)}px`;
}

export function searchableSelect({
  placeholder = 'Select...',
  searchPlaceholder = '',
  ariaLabel = '',
  value = '',
  options = [],
  clearable = true,
  compact = true,
  wide = false,
  style = {},
  className = '',
  minDropdownWidth = 220,
  maxDropdownWidth = 380,
  onChange = null,
} = {}) {
  let currentValue = value != null ? String(value) : '';
  let allOptions = [];
  let filtered = [];
  let isOpen = false;
  let isDisabled = false;
  let focusedIndex = -1;
  let searchQuery = '';

  const inputPlaceholder = searchPlaceholder || `Search ${ariaLabel || placeholder}...`.replace(/\.{2,}$/, '').trim();

  const trigger = h('div', {
    class: `search-select${compact ? '' : ' lg'}${wide ? ' wide' : ''}${className ? ' ' + className : ''}`,
    role: 'combobox',
    'aria-haspopup': 'listbox',
    'aria-expanded': 'false',
    'aria-label': ariaLabel || placeholder,
    tabindex: '0',
    style,
  });

  const labelEl = h('span', { class: 'search-select-label' });
  const clearBtn = h('button', {
    type: 'button',
    class: 'search-select-clear',
    title: 'Clear selection',
    tabindex: '-1',
    style: { display: 'none' },
    onclick: (e) => {
      e.stopPropagation();
      setValue('', true);
    },
  }, icon('x'));

  const arrowEl = h('span', { class: 'search-select-arrow' }, icon('chevron-down'));
  const actionsWrap = h('span', { class: 'search-select-actions' }, clearBtn, arrowEl);

  trigger.append(labelEl, actionsWrap);

  let popover = null;
  let searchInput = null;
  let listEl = null;
  let countBadge = null;
  let inputClearBtn = null;

  function normalizeOption(item) {
    if (item == null) return null;
    if (typeof item === 'string') {
      let badge = '';
      if (item.includes('/')) {
        badge = item.split('/')[0];
      }
      return { value: item, label: item, badge, group: '', isAll: false };
    }
    if (typeof item === 'object') {
      const val = item.value != null ? String(item.value) : String(item.id || item.key || item.name || '');
      const lbl = item.label || item.name || item.id || val;
      let badge = item.badge || item.provider_id || item.provider || '';
      if (!badge && val.includes('/')) {
        badge = val.split('/')[0];
      }
      return {
        value: val,
        label: String(lbl),
        badge: String(badge || ''),
        group: String(item.group || ''),
        isAll: Boolean(item.isAll),
      };
    }
    return { value: String(item), label: String(item), badge: '', group: '', isAll: false };
  }

  function findOption(val) {
    return allOptions.find((o) => o.value === val);
  }

  function updateTriggerDisplay() {
    const opt = findOption(currentValue);
    const hasVal = currentValue !== '';
    trigger.classList.toggle('has-value', hasVal);

    if (hasVal) {
      const text = opt ? opt.label : currentValue;
      labelEl.textContent = text;
      labelEl.classList.remove('placeholder');
      trigger.title = text;
      clearBtn.style.display = clearable ? 'inline-flex' : 'none';
    } else {
      const allOpt = allOptions.find((o) => o.value === '' || o.isAll);
      const text = allOpt ? allOpt.label : placeholder;
      labelEl.textContent = text;
      labelEl.classList.add('placeholder');
      trigger.title = text;
      clearBtn.style.display = 'none';
    }
  }

  function setValue(newVal, emit = false) {
    const nextVal = newVal != null ? String(newVal) : '';
    if (currentValue === nextVal && !emit) return;
    currentValue = nextVal;
    updateTriggerDisplay();
    if (emit) {
      const opt = findOption(currentValue);
      onChange?.(currentValue, opt);
      try {
        if (typeof CustomEvent === 'function' && typeof trigger.dispatchEvent === 'function') {
          trigger.dispatchEvent(new CustomEvent('change', { detail: { value: currentValue, option: opt } }));
        }
      } catch {}
    }
  }

  function setOptions(newOpts = [], newVal = undefined, allLabel = undefined) {
    const normalized = (Array.isArray(newOpts) ? newOpts : [])
      .map(normalizeOption)
      .filter(Boolean);

    if (allLabel != null && allLabel !== false) {
      const hasEmpty = normalized.some((o) => o.value === '');
      if (!hasEmpty) {
        normalized.unshift({
          value: '',
          label: typeof allLabel === 'string' ? allLabel : placeholder,
          badge: '',
          group: '',
          isAll: true,
        });
      }
    }

    allOptions = normalized;

    if (newVal !== undefined) {
      currentValue = String(newVal || '');
      if (currentValue && !allOptions.some((o) => o.value === currentValue)) {
        allOptions.push(normalizeOption(currentValue));
      }
    }

    updateTriggerDisplay();
    if (isOpen) {
      renderPopoverList();
      reposition();
    }
  }

  function createPopover() {
    searchInput = h('input', {
      class: 'search-select-input',
      type: 'text',
      autocomplete: 'off',
      spellcheck: 'false',
      placeholder: inputPlaceholder,
      'aria-label': inputPlaceholder,
    });

    countBadge = h('span', { class: 'search-select-count' });

    inputClearBtn = h('button', {
      type: 'button',
      class: 'search-select-clear',
      title: 'Clear search',
      style: { display: 'none' },
      onclick: (e) => {
        e.stopPropagation();
        searchInput.value = '';
        searchQuery = '';
        inputClearBtn.style.display = 'none';
        focusedIndex = 0;
        renderPopoverList();
        searchInput.focus();
      },
    }, icon('x'));

    const header = h('div', { class: 'search-select-header' },
      icon('search'),
      searchInput,
      countBadge,
      inputClearBtn
    );

    listEl = h('div', { class: 'search-select-list', role: 'listbox' });

    popover = h('div', { class: 'search-select-popover' }, header, listEl);

    searchInput.addEventListener('input', () => {
      searchQuery = searchInput.value.trim().toLowerCase();
      inputClearBtn.style.display = searchQuery ? 'inline-flex' : 'none';
      focusedIndex = 0;
      renderPopoverList();
    });

    searchInput.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        moveFocus(1);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        moveFocus(-1);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        selectFocused();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        close();
        trigger.focus();
      } else if (e.key === 'Tab') {
        close();
      }
    });
  }

  function getFilteredOptions() {
    if (!searchQuery) return allOptions.slice();
    const qWords = searchQuery.split(/\s+/).filter(Boolean);
    return allOptions.filter((opt) => {
      const target = `${opt.label} ${opt.value} ${opt.badge || ''} ${opt.group || ''}`.toLowerCase();
      return qWords.every((w) => target.includes(w));
    });
  }

  function renderPopoverList() {
    filtered = getFilteredOptions();

    if (countBadge) {
      if (searchQuery) {
        countBadge.textContent = `${filtered.length} / ${allOptions.length}`;
      } else {
        countBadge.textContent = `${allOptions.length}`;
      }
    }

    if (!filtered.length) {
      listEl.replaceChildren(
        h('div', { class: 'search-select-empty' },
          h('div', null, 'No options found'),
          h('span', null, `No match for "${searchInput.value}"`),
          h('button', {
            class: 'btn btn-sm',
            type: 'button',
            onclick: () => {
              searchInput.value = '';
              searchQuery = '';
              inputClearBtn.style.display = 'none';
              focusedIndex = 0;
              renderPopoverList();
              searchInput.focus();
            },
          }, 'Clear filter')
        )
      );
      return;
    }

    const queryTerms = searchQuery ? searchQuery.split(/\s+/).filter(Boolean) : [];

    const rows = filtered.map((opt, idx) => {
      const isSelected = opt.value === currentValue;
      const isFocused = idx === focusedIndex;

      const row = h('div', {
        class: `search-select-option${isSelected ? ' selected' : ''}${isFocused ? ' focused' : ''}${opt.isAll ? ' is-all' : ''}`,
        role: 'option',
        'aria-selected': isSelected ? 'true' : 'false',
        title: opt.label || opt.value,
        onmousemove: () => {
          if (focusedIndex !== idx) {
            focusedIndex = idx;
            updateFocusedClass();
          }
        },
        onclick: (e) => {
          e.stopPropagation();
          setValue(opt.value, true);
          close();
          trigger.focus();
        },
      });

      const textSpan = h('span', { class: 'search-select-option-text' },
        ...highlightMatches(opt.label, queryTerms)
      );
      row.append(textSpan);

      if (opt.badge) {
        row.append(h('span', { class: 'search-select-option-badge' }, opt.badge));
      }

      if (isSelected) {
        row.append(icon('check', 'search-select-option-check'));
      }

      return row;
    });

    listEl.replaceChildren(...rows);

    const targetRow = rows[focusedIndex >= 0 ? focusedIndex : 0];
    if (targetRow) {
      targetRow.scrollIntoView({ block: 'nearest' });
    }
  }

  function moveFocus(delta) {
    if (!filtered.length) return;
    focusedIndex += delta;
    if (focusedIndex < 0) focusedIndex = filtered.length - 1;
    if (focusedIndex >= filtered.length) focusedIndex = 0;
    updateFocusedClass();
    const children = listEl.children;
    if (children[focusedIndex]) {
      children[focusedIndex].scrollIntoView({ block: 'nearest' });
    }
  }

  function updateFocusedClass() {
    const children = listEl.children;
    for (let i = 0; i < children.length; i++) {
      children[i].classList.toggle('focused', i === focusedIndex);
    }
  }

  function selectFocused() {
    if (focusedIndex >= 0 && focusedIndex < filtered.length) {
      const opt = filtered[focusedIndex];
      setValue(opt.value, true);
      close();
      trigger.focus();
    }
  }

  function open() {
    if (isDisabled || isOpen) return;
    document.querySelectorAll('.search-select.is-open').forEach((el) => el.close?.());
    document.querySelectorAll('.search-select-popover').forEach((p) => p.remove());

    isOpen = true;
    trigger.classList.add('is-open');
    trigger.setAttribute('aria-expanded', 'true');

    if (!popover) createPopover();
    searchQuery = '';
    if (searchInput) searchInput.value = '';
    if (inputClearBtn) inputClearBtn.style.display = 'none';

    const selIdx = allOptions.findIndex((o) => o.value === currentValue);
    focusedIndex = selIdx >= 0 ? selIdx : 0;

    renderPopoverList();
    document.body.append(popover);
    reposition();

    requestAnimationFrame(() => {
      if (searchInput) searchInput.focus();
    });

    setTimeout(() => {
      document.addEventListener('mousedown', onDocMouseDown, true);
      window.addEventListener('resize', onWinResize);
      window.addEventListener('scroll', onWinScroll, true);
    }, 10);
  }

  function close() {
    if (!isOpen) return;
    isOpen = false;
    trigger.classList.remove('is-open');
    trigger.setAttribute('aria-expanded', 'false');
    if (popover && popover.parentNode) {
      popover.remove();
    }
    document.removeEventListener('mousedown', onDocMouseDown, true);
    window.removeEventListener('resize', onWinResize);
    window.removeEventListener('scroll', onWinScroll, true);
  }

  function onDocMouseDown(e) {
    if (trigger.contains(e.target) || (popover && popover.contains(e.target))) {
      return;
    }
    close();
  }

  function onWinResize() {
    if (isOpen) reposition();
  }

  function onWinScroll(e) {
    if (!isOpen) return;
    if (popover && popover.contains(e.target)) return;
    reposition();
  }

  function reposition() {
    if (!isOpen || !popover || !trigger.isConnected) {
      if (!trigger.isConnected) close();
      return;
    }
    positionPopover(trigger, popover, minDropdownWidth, maxDropdownWidth);
  }

  trigger.addEventListener('click', () => {
    if (isDisabled) return;
    if (isOpen) close();
    else open();
  });

  trigger.addEventListener('keydown', (e) => {
    if (isDisabled) return;
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!isOpen) open();
    }
  });

  Object.defineProperty(trigger, 'value', {
    get() { return currentValue; },
    set(v) { setValue(v, false); },
    configurable: true,
  });

  Object.defineProperty(trigger, 'disabled', {
    get() { return isDisabled; },
    set(v) {
      isDisabled = Boolean(v);
      trigger.classList.toggle('disabled', isDisabled);
      trigger.setAttribute('aria-disabled', isDisabled ? 'true' : 'false');
      trigger.tabIndex = isDisabled ? -1 : 0;
      if (isDisabled && isOpen) close();
    },
    configurable: true,
  });

  trigger.setValue = (val, emit = false) => setValue(val, emit);
  trigger.setOptions = (newOpts, val, allLabel) => setOptions(newOpts, val, allLabel);
  trigger.open = open;
  trigger.close = close;

  setOptions(options, value);
  return trigger;
}

export { setRoute } from './state.js';
