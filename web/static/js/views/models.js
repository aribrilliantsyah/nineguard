// Models view: View, filter by provider, enable/disable firewall, and sync models from active providers.
import { api } from '../api.js';
import { h, icon, emptyState, toast, formDialog, confirmDialog, fmtNum, fmtCompact, fmtAgo } from '../ui.js';

export function mount(root) {
  let alive = true;
  let allModels = [];
  let providersList = [];
  let selectedProvider = '';
  let filterText = '';
  let page = 1;
  let pageSize = 20;

  const tableCard = h('div', { class: 'card table-card' });

  const searchInput = h('input', {
    class: 'input',
    type: 'text',
    placeholder: 'Search models...',
    spellcheck: 'false',
    style: { width: '200px' },
    oninput: (e) => {
      filterText = e.target.value.toLowerCase().trim();
      page = 1;
      renderTable();
    }
  });

  const providerSelect = h('select', {
    class: 'select',
    style: { maxWidth: '200px' },
    onchange: (e) => {
      selectedProvider = e.target.value;
      page = 1;
      renderTable();
    }
  }, h('option', { value: '' }, 'All Providers'));

  const fetchBtn = h('button', {
    class: 'btn',
    type: 'button',
    onclick: fetchFromProviders
  }, icon('refresh'), 'Fetch from Providers');

  const addBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    onclick: addModel
  }, icon('plus'), 'Add Model');

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Models Management'),
        h('p', null, 'Control model firewall rules and access. Models are grouped by provider prefix (e.g. openrouter/model-id).')
      ),
      h('div', { class: 'page-actions' },
        searchInput,
        providerSelect,
        fetchBtn,
        addBtn
      )
    ),
    tableCard
  ));

  async function fetchFromProviders() {
    fetchBtn.disabled = true;
    fetchBtn.textContent = 'Fetching...';
    try {
      const res = await api.post('/models/sync');
      let msg = `Synced successfully! ${res.added || 0} models registered.`;
      if (res.removed > 0) {
        msg += ` (${res.removed} obsolete models removed)`;
      }
      toast(msg, 'ok');
      await load();
    } catch (e) {
      toast(`Fetch failed: ${e.message}`, 'error');
    } finally {
      fetchBtn.disabled = false;
      fetchBtn.replaceChildren(icon('refresh'), 'Fetch from Providers');
    }
  }

  async function addModel() {
    const res = await formDialog({
      title: 'Add Model Identifier',
      submitText: 'Save Model',
      fields: [
        {
          name: 'model',
          label: 'Model Identifier',
          placeholder: 'e.g. openrouter/anthropic/claude-3.5-sonnet or gpt-4o',
          required: true
        }
      ],
      onSubmit: async (v) => {
        const m = v.model.trim();
        if (!m) throw new Error('Model identifier is required');
        await api.post('/models/toggle', { model: m, enabled: true });
        return m;
      }
    });
    if (!res) return;
    toast(`Model ${res} added and enabled`, 'ok');
    load();
  }

  async function toggleModel(m) {
    const newStatus = !m.enabled;
    try {
      await api.post('/models/toggle', { model: m.id, enabled: newStatus });
      m.enabled = newStatus;
      toast(`Model ${m.id} is now ${newStatus ? 'ENABLED' : 'DISABLED'}`);
      renderTable();
    } catch (e) {
      toast(`Failed to update model: ${e.message}`, 'error');
    }
  }

  async function confirmDeleteModel(m) {
    const ok = await confirmDialog({
      title: `Delete Model "${m.id}"?`,
      body: 'This will remove the model from NineGuard local database. It can be re-added anytime or re-synced from its provider.',
      confirmText: 'Delete Model',
      danger: true,
    });
    if (!ok) return;

    try {
      await api.del(`/models/${encodeURIComponent(m.id)}`);
      toast(`Model "${m.id}" deleted from NineGuard`, 'ok');
      await load();
    } catch (e) {
      toast(`Failed to delete: ${e.message}`, 'error');
    }
  }

  function getProviderLabel(provID) {
    const p = providersList.find(pr => pr.id === provID || pr.prefix === provID);
    return p ? (p.name || p.prefix || provID) : provID;
  }

  function renderTable() {
    let filtered = allModels.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(filterText) || (m.name && m.name.toLowerCase().includes(filterText));
      if (!matchesSearch) return false;
      if (!selectedProvider) return true;
      return m.provider_id === selectedProvider || m.id.startsWith(selectedProvider + '/');
    });

    if (!filtered.length) {
      tableCard.replaceChildren(
        emptyState('box', 'No models found', filterText || selectedProvider ? 'No model matches your filter.' : 'Click "Fetch from Providers" to retrieve all available models from your OpenAI-compatible providers.')
      );
      return;
    }

    const total = filtered.length;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    if (page > totalPages) page = totalPages;
    if (page < 1) page = 1;

    const startIdx = (page - 1) * pageSize;
    const endIdx = Math.min(startIdx + pageSize, total);
    const pagedModels = filtered.slice(startIdx, endIdx);

    const rows = pagedModels.map((m) => {
      const statusBadge = m.enabled
        ? h('span', { class: 'badge ok' }, icon('check'), 'Enabled')
        : h('span', { class: 'badge err' }, icon('alert'), 'Disabled');

      const actionBtn = h('button', {
        class: `btn btn-sm ${m.enabled ? 'btn-danger' : 'btn-primary'}`,
        type: 'button',
        onclick: () => toggleModel(m)
      }, m.enabled ? 'Disable' : 'Enable');

      const deleteBtn = h('button', {
        class: 'btn btn-sm btn-danger',
        type: 'button',
        title: 'Delete from NineGuard',
        onclick: () => confirmDeleteModel(m)
      }, icon('trash'));

      const lastUsed = m.last_used_at ? fmtAgo(Date.parse(m.last_used_at)) : '-';

      const provID = m.provider_id || (m.id.includes('/') ? m.id.split('/')[0] : '');
      const provBadge = provID
        ? h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: '600' }, title: getProviderLabel(provID) },
            icon('server'), ' ', provID
          )
        : h('span', { class: 'badge muted' }, 'root');

      return h('tr', null,
        h('td', null,
          h('div', { class: 'strong' }, m.id),
          m.name && m.name !== m.id ? h('div', { class: 'sub' }, m.name) : null
        ),
        h('td', null, provBadge),
        h('td', null, statusBadge),
        h('td', { class: 'num' }, fmtNum(m.total_requests || 0)),
        h('td', { class: 'num' }, fmtCompact(m.total_tokens || 0)),
        h('td', { class: 'muted' }, lastUsed),
        h('td', { class: 'num' },
          h('div', { style: { display: 'inline-flex', gap: '6px' } }, actionBtn, deleteBtn)
        )
      );
    });

    function changePage(newPage) {
      page = newPage;
      renderTable();
      tableCard.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }

    const startNum = startIdx + 1;
    const countInfo = filtered.length < allModels.length
      ? `Showing ${startNum}–${endIdx} of ${fmtNum(total)} models (${fmtNum(allModels.length)} total)`
      : `Showing ${startNum}–${endIdx} of ${fmtNum(total)} models`;

    const prevBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      disabled: page <= 1,
      onclick: () => changePage(page - 1)
    }, 'Previous');

    const nextBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      disabled: page >= totalPages,
      onclick: () => changePage(page + 1)
    }, 'Next');

    const pageButtons = [];
    let startP = Math.max(1, page - 2);
    let endP = Math.min(totalPages, startP + 4);
    if (endP - startP < 4) {
      startP = Math.max(1, endP - 4);
    }

    if (startP > 1) {
      pageButtons.push(h('button', {
        class: 'btn btn-sm',
        type: 'button',
        style: { minWidth: '28px', padding: '0 6px' },
        onclick: () => changePage(1)
      }, '1'));
      if (startP > 2) {
        pageButtons.push(h('span', { class: 'muted', style: { padding: '0 2px', fontSize: '11px' } }, '…'));
      }
    }

    for (let p = startP; p <= endP; p++) {
      const isCurrent = p === page;
      pageButtons.push(h('button', {
        class: `btn btn-sm ${isCurrent ? 'btn-primary' : ''}`,
        type: 'button',
        style: {
          minWidth: '28px',
          padding: '0 6px',
          fontWeight: isCurrent ? '700' : 'normal',
          pointerEvents: isCurrent ? 'none' : 'auto'
        },
        onclick: () => changePage(p)
      }, String(p)));
    }

    if (endP < totalPages) {
      if (endP < totalPages - 1) {
        pageButtons.push(h('span', { class: 'muted', style: { padding: '0 2px', fontSize: '11px' } }, '…'));
      }
      pageButtons.push(h('button', {
        class: 'btn btn-sm',
        type: 'button',
        style: { minWidth: '28px', padding: '0 6px' },
        onclick: () => changePage(totalPages)
      }, String(totalPages)));
    }

    const pageSizeSelect = h('select', {
      class: 'select',
      style: { height: '26px', padding: '0 20px 0 8px', fontSize: '11.5px', width: 'auto' },
      onchange: (e) => {
        pageSize = Number(e.target.value);
        page = 1;
        renderTable();
      }
    },
      h('option', { value: '10', selected: pageSize === 10 }, '10 / page'),
      h('option', { value: '20', selected: pageSize === 20 }, '20 / page'),
      h('option', { value: '50', selected: pageSize === 50 }, '50 / page'),
      h('option', { value: '100', selected: pageSize === 100 }, '100 / page')
    );

    const paginationBar = h('div', {
      class: 'table-foot',
      style: {
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        borderTop: '1px solid var(--border)',
        padding: '10px 8px 6px',
        flexWrap: 'wrap',
        gap: '10px'
      }
    },
      h('span', null, countInfo),
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
        pageSizeSelect,
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px' } },
          prevBtn,
          ...pageButtons,
          nextBtn
        )
      )
    );

    tableCard.replaceChildren(
      h('div', { class: 'table-wrap' },
        h('table', { class: 'table' },
          h('thead', null,
            h('tr', null,
              h('th', null, 'Model Identifier'),
              h('th', null, 'Provider'),
              h('th', null, 'Firewall Status'),
              h('th', { class: 'num' }, 'Total Requests'),
              h('th', { class: 'num' }, 'Tokens Used'),
              h('th', null, 'Last Request'),
              h('th', { class: 'num' }, 'Action')
            )
          ),
          h('tbody', null, ...rows)
        )
      ),
      paginationBar
    );
  }

  async function load() {
    tableCard.replaceChildren(h('div', { class: 'skel', style: { height: '240px' } }));
    try {
      const [mRes, pRes] = await Promise.all([
        api.get('/models'),
        api.get('/providers').catch(() => ({ providers: [] }))
      ]);
      allModels = Array.isArray(mRes) ? mRes : [];
      providersList = (pRes && pRes.providers) ? pRes.providers : [];

      providerSelect.replaceChildren(
        h('option', { value: '' }, 'All Providers'),
        ...providersList.map(p => h('option', { value: p.prefix || p.id }, `${p.name} (${p.prefix || 'root'})`))
      );
    } catch (e) {
      if (alive) tableCard.replaceChildren(emptyState('alert', 'Could not load models', e.message));
      return;
    }
    if (alive) renderTable();
  }

  load();
  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
