// Quotas: every API key with its Key Quota or Model Quotas, usage and reset (ADR 0007).
import { api } from '../api.js';
import { h, icon, emptyState, fmtNum, debounce } from '../ui.js';
import { patchRoute } from '../state.js';
import { formatTokensCompact } from '../quotahelpers.js';
import { barTone, formatResetIn, filterQuotaKeys, quotaFiltersFromParams } from '../modelquotas.js';
import { renderUpstreamQuotas } from './upstream_quotas.js';
import { openGlobalScheduleDialog } from './globalschedule.js';
import { nextResetText } from '../resetschedule.js';

const PERIOD_LABEL = { daily: 'daily', weekly: 'weekly', monthly: 'monthly', total: 'lifetime' };
const REASON_LABEL = { removed: 'removed', disabled: 'disabled', no_access: 'no access' };

export function mount(root) {
  let alive = true;
  let data = null;
  let error = '';
  let filters = quotaFiltersFromParams({});
  const toggled = new Map(); // key id -> expanded (user choice beats the default)

  const cards = h('div', { class: 'grid grid-4 quota-summary' });
  const search = h('input', { class: 'input', type: 'search', placeholder: 'Search key or model...', style: { width: '220px' } });
  const modeSel = h('select', { class: 'select' }, [['all', 'Mode: all'], ['key', 'Key Quota'], ['model', 'Per Model']].map(([v, l]) => h('option', { value: v }, l)));
  const statusSel = h('select', { class: 'select' }, [['all', 'Status: all'], ['near', 'Near limit'], ['exhausted', 'Exhausted'], ['inactive', 'Inactive']].map(([v, l]) => h('option', { value: v }, l)));
  const unlimited = h('input', { type: 'checkbox' });
  const body = h('div', { class: 'card table-card' });

  search.addEventListener('input', debounce(() => patchRoute({ q: search.value }, { replace: true }), 250));
  modeSel.addEventListener('change', () => patchRoute({ mode: modeSel.value === 'all' ? '' : modeSel.value }));
  statusSel.addEventListener('change', () => patchRoute({ status: statusSel.value === 'all' ? '' : statusSel.value }));
  unlimited.addEventListener('change', () => patchRoute({ unlimited: unlimited.checked ? '1' : '' }));

  let activeTab = 'client';
  const clientTabBtn = h('button', {
    class: 'btn btn-sm btn-primary',
    type: 'button',
    onclick: () => switchTab('client')
  }, 'Client Quotas');
  const upstreamTabBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    onclick: () => switchTab('upstream')
  }, 'Upstream Quotas');
  const tabSwitcher = h('div', { class: 'btn-group mb', style: { display: 'inline-flex' } }, clientTabBtn, upstreamTabBtn);

  const clientContainer = h('div', null,
    cards,
    h('div', { class: 'toolbar mt', style: { borderBottom: '0', paddingLeft: 0 } }, search, modeSel, statusSel,
      h('label', { class: 'muted', style: { display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '12px' } }, unlimited, 'Show unlimited')),
    body
  );
  const upstreamContainer = h('div', { style: { display: 'none' } });
  // Global Reset Schedule (ADR 0010): applies to client key quotas only, so it hides with the Upstream tab.
  const scheduleBtn = h('button', {
    class: 'btn btn-sm', type: 'button', title: 'When client key quotas reset (all keys without their own schedule)',
    onclick: () => openGlobalScheduleDialog({ onChanged: () => load() }),
  }, icon('clock'), 'Reset schedule');

  function switchTab(tab) {
    activeTab = tab;
    if (tab === 'upstream') {
      clientTabBtn.className = 'btn btn-sm';
      upstreamTabBtn.className = 'btn btn-sm btn-primary';
      clientContainer.style.display = 'none';
      scheduleBtn.style.display = 'none';
      upstreamContainer.style.display = 'block';
      renderUpstreamQuotas(upstreamContainer);
    } else {
      clientTabBtn.className = 'btn btn-sm btn-primary';
      upstreamTabBtn.className = 'btn btn-sm';
      clientContainer.style.display = 'block';
      scheduleBtn.style.display = '';
      upstreamContainer.style.display = 'none';
    }
  }

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Quotas & Limits'),
        h('p', null, 'Token limits per client key/model and live rolling capacity from upstream providers.')
      ),
      h('div', { class: 'page-actions' }, scheduleBtn, tabSwitcher)
    ),
    clientContainer,
    upstreamContainer
  ));

  function card(label, value, tone) {
    return h('div', { class: 'card' }, h('small', { class: 'muted' }, label),
      h('div', { style: { fontSize: '22px', fontWeight: '700', color: tone ? `var(--${tone})` : '' } }, fmtNum(value)));
  }

  function bar(status, percent) {
    return h('span', null, h('span', { class: `qbar ${barTone(status)}` }, h('i', { style: { width: `${Math.min(100, percent)}%` } })),
      h('span', { class: `qpct ${barTone(status)}` }, `${percent}%`));
  }

  // tz: the timezone of the key's Effective Reset Schedule (UTC for the default). The relative
  // text stays; the tooltip shows the exact reset in that zone.
  function resetCell(r, tz = 'UTC') {
    if (!r.reset_at) return h('td', { class: 'muted' }, 'never');
    return h('td', { class: 'muted', title: nextResetText(r.reset_at, tz) }, `in ${formatResetIn(r.reset_at)}`);
  }

  function modelRow(r, tz) {
    const inactive = r.inactive;
    return h('tr', { class: inactive ? 'qrow-inactive' : '' },
      h('td', { style: { paddingLeft: '30px' } }, h('code', { style: { fontSize: '12px' } }, r.model_id), ' ',
        inactive ? h('span', { class: 'badge removed' }, REASON_LABEL[r.inactive_reason] || 'inactive')
          : r.status === 'exhausted' ? h('span', { class: 'badge err' }, 'exhausted') : null),
      h('td'),
      h('td', null, inactive ? '' : `${formatTokensCompact(r.usage)} / ${formatTokensCompact(r.quota_limit)}`),
      h('td', null, inactive ? h('span', { class: 'muted' }, 'not enforced') : bar(r.status, r.percent)),
      h('td', { class: 'muted' }, PERIOD_LABEL[r.quota_period] || r.quota_period),
      inactive ? h('td') : resetCell(r, tz));
  }

  const keyTz = (k) => (k.effective_quota_reset_schedule ? k.effective_quota_reset_schedule.timezone : 'UTC');

  function keyRows(k) {
    const open = toggled.has(k.id) ? toggled.get(k.id) : (k.status === 'near' || k.status === 'exhausted');
    const nameLink = h('a', { href: `#/endpoints?edit=${encodeURIComponent(k.id)}`, class: 'strong', title: 'Edit this key' }, k.name);
    const disabled = k.is_active ? null : h('span', { class: 'badge', style: { marginLeft: '6px' } }, 'disabled');

    if (k.quota_mode === 'model') {
      const rows = k.model_quotas || [];
      const head = h('tr', null,
        h('td', null,
          h('button', { class: 'btn btn-sm', type: 'button', 'aria-expanded': String(open), style: { marginRight: '6px' }, onclick: () => { toggled.set(k.id, !open); render(); } },
            icon(open ? 'chevron-down' : 'chevron-right')),
          nameLink, disabled, h('span', { class: 'muted', style: { marginLeft: '6px' } }, `· ${rows.length} limit${rows.length === 1 ? '' : 's'}`)),
        h('td', null, h('span', { class: 'badge' }, 'per model')),
        h('td', { class: 'muted' }, k.status === 'unlimited' ? 'nothing enforced' : 'highest of active'),
        h('td', null, k.status === 'unlimited' ? '' : bar(k.status, k.top_percent)),
        h('td'), h('td'));
      return [head, ...(open ? rows.map((r) => modelRow(r, keyTz(k))) : [])];
    }
    const q = k.key_quota;
    return [h('tr', null,
      h('td', null, nameLink, disabled),
      h('td', null, h('span', { class: 'badge' }, 'key')),
      h('td', null, q ? `${formatTokensCompact(q.usage)} / ${formatTokensCompact(q.quota_limit)}` : h('span', { class: 'muted' }, 'unlimited')),
      h('td', null, q ? bar(q.status, q.percent) : ''),
      h('td', { class: 'muted' }, q ? (PERIOD_LABEL[q.quota_period] || q.quota_period) : ''),
      q ? resetCell(q, keyTz(k)) : h('td'))];
  }

  function render() {
    if (!alive) return;
    if (error) { body.replaceChildren(emptyState('alert', 'Could not load quotas', error)); cards.replaceChildren(); return; }
    if (!data) return;
    const s = data.summary;
    cards.replaceChildren(card('Keys with quotas', s.keys_with_quota), card('Model Quotas', s.model_quotas),
      card(`Near limit (>=${data.warn_percent}%)`, s.near_limit, s.near_limit ? 'lvt-warn' : ''), card('Exhausted', s.exhausted, s.exhausted ? 'danger' : ''));

    const list = filterQuotaKeys(data.keys, filters);
    if (!list.length) {
      body.replaceChildren(emptyState('search', 'No keys match', filters.status !== 'all' || filters.q || filters.mode !== 'all'
        ? 'Clear the search or filters.' : 'No API key has a quota yet. Set one in Endpoints & Keys.'));
      return;
    }
    body.replaceChildren(h('div', { class: 'table-wrap' }, h('table', { class: 'table quota-table' },
      h('thead', null, h('tr', null, ['API key / Model', 'Mode', 'Used / Limit', 'Usage', 'Period', 'Resets'].map((t) => h('th', null, t)))),
      h('tbody', null, list.flatMap(keyRows)))));
  }

  async function load() {
    try { data = await api.get('/quotas'); error = ''; } catch (e) { data = null; error = e.message; }
    render();
  }

  function applyFilters() {
    if (document.activeElement !== search) search.value = filters.q;
    modeSel.value = filters.mode;
    statusSel.value = filters.status;
    unlimited.checked = filters.unlimited === '1';
  }

  load();
  return {
    update(params) {
      const next = quotaFiltersFromParams(params);
      if (JSON.stringify(next) === JSON.stringify(filters) && data) return;
      filters = next;
      applyFilters();
      render();
    },
    refresh: load,
    destroy() { alive = false; },
  };
}
