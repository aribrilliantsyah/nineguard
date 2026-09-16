// Ctrl+K command palette: jump to any page or run an action.
// items: [{group, label, hint, icon, keywords, run}], built on open.
import { h, icon } from './ui.js';

let openEl = null;

// Scores how well q matches an item: text in the label beats text in its
// keywords, which beats the letters of q in order inside the label ("dsb").
function score(q, it) {
  if (!q) return 1;
  const label = it.label.toLowerCase();
  const i = label.indexOf(q);
  if (i >= 0) return 1000 - i - label.length / 100;
  const all = `${label} ${it.keywords || ''} ${it.hint || ''}`.toLowerCase();
  const words = q.split(/\s+/);
  if (words.every((w) => all.includes(w))) return 500 - label.length / 100;
  let pos = 0;
  let gaps = 0;
  for (const ch of q.replace(/\s+/g, '')) {
    const j = label.indexOf(ch, pos);
    if (j < 0) return 0;
    gaps += j - pos;
    pos = j + 1;
  }
  return gaps <= label.length / 2 ? 100 - gaps : 0;
}

export function openPalette(build) {
  if (openEl) return;
  const input = h('input', { class: 'palette-input', type: 'text', autocomplete: 'off', spellcheck: 'false', placeholder: 'Go to a page or run an action', 'aria-label': 'Search menu' });
  const list = h('div', { class: 'palette-list', role: 'listbox' });
  const foot = h('div', { class: 'palette-foot' },
    h('span', null, h('kbd', null, '↑↓'), ' move'), h('span', null, h('kbd', null, 'Enter'), ' open'), h('span', null, h('kbd', null, 'Esc'), ' close'));
  const box = h('div', { class: 'palette', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Command menu' },
    h('div', { class: 'palette-search' }, icon('search'), input), list, foot);
  const overlay = h('div', { class: 'overlay palette-overlay', onmousedown: (e) => { if (e.target === overlay) close(); } }, box);
  let shown = [];
  let sel = 0;

  function close() {
    overlay.remove();
    openEl = null;
  }

  function render() {
    const q = input.value.trim().toLowerCase();
    const items = build(input.value.trim());
    shown = items
      .map((it) => ({ it, s: it.always ? 2000 : score(q, it) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => (q ? b.s - a.s : 0))
      .slice(0, 60)
      .map((x) => x.it);
    sel = Math.min(sel, Math.max(0, shown.length - 1));
    if (!shown.length) {
      list.replaceChildren(h('div', { class: 'palette-empty' }, 'Nothing matches'));
      return;
    }
    const rows = [];
    let group = null;
    shown.forEach((it, i) => {
      if (!q && it.group !== group) {
        group = it.group;
        rows.push(h('div', { class: 'palette-group' }, group));
      }
      rows.push(h('button', {
        type: 'button', class: `palette-item${i === sel ? ' sel' : ''}`, role: 'option',
        onmousemove: () => { if (sel !== i) { sel = i; mark(); } },
        onclick: () => run(i),
      }, icon(it.icon || 'arrow-right'), h('span', { class: 'label' }, it.label), it.hint ? h('span', { class: 'hint' }, it.hint) : null));
    });
    list.replaceChildren(...rows);
  }

  function mark() {
    list.querySelectorAll('.palette-item').forEach((b, i) => b.classList.toggle('sel', i === sel));
    list.querySelectorAll('.palette-item')[sel]?.scrollIntoView({ block: 'nearest' });
  }

  function run(i) {
    const it = shown[i];
    if (!it) return;
    close();
    it.run();
  }

  input.addEventListener('input', () => { sel = 0; render(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, shown.length - 1); mark(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); mark(); }
    else if (e.key === 'Enter') { e.preventDefault(); run(sel); }
    else if (e.key === 'Escape') { e.preventDefault(); close(); }
  });

  document.body.append(overlay);
  openEl = overlay;
  render();
  input.focus();
}
