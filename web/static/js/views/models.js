// Models & Groups view: View, filter by provider, toggle firewall, and manage Model Groups.
import { api } from '../api.js';
import { h, icon, emptyState, toast, formDialog, confirmDialog, fmtNum, fmtCompact, fmtAgo, searchableSelect } from '../ui.js';

export function mount(root) {
  let alive = true;
  let activeTab = 'models'; // 'models' | 'groups'
  let allModels = [];
  let modelGroups = [];
  let providersList = [];
  let selectedProvider = '';
  let filterText = '';
  let groupFilterText = '';
  let page = 1;
  let pageSize = 20;

  const contentWrap = h('div', { class: 'tab-body' });
  const actionsWrap = h('div', { class: 'page-actions' });
  const subText = h('p', null, 'Control model firewall rules and access. Models are grouped by provider prefix (e.g. openrouter/model-id).');

  // ── Tab Navigation ──
  const tabModelsBadge = h('span', { class: 'badge' }, '0');
  const tabGroupsBadge = h('span', { class: 'badge' }, '0');

  const tabModelsBtn = h('button', {
    class: 'tab active',
    type: 'button',
    onclick: () => switchTab('models')
  }, icon('box'), 'All Models', tabModelsBadge);

  const tabGroupsBtn = h('button', {
    class: 'tab',
    type: 'button',
    onclick: () => switchTab('groups')
  }, icon('sparkles'), 'Model Groups', tabGroupsBadge);

  const tabsNav = h('div', { class: 'tabs' }, tabModelsBtn, tabGroupsBtn);

  // ── All Models Controls ──
  const searchInput = h('input', {
    class: 'input',
    type: 'text',
    placeholder: 'Search models...',
    spellcheck: 'false',
    style: { width: '200px' },
    oninput: (e) => {
      filterText = e.target.value.toLowerCase().trim();
      page = 1;
      renderModelsTable();
    }
  });

  const providerSelect = searchableSelect({
    placeholder: 'All Providers',
    searchPlaceholder: 'Search providers...',
    ariaLabel: 'Filter by Provider',
    clearable: true,
    compact: true,
    style: { minWidth: '150px', maxWidth: '210px' },
    onChange: (val) => {
      selectedProvider = val;
      page = 1;
      renderModelsTable();
    }
  });

  const fetchBtn = h('button', {
    class: 'btn',
    type: 'button',
    onclick: fetchFromProviders
  }, icon('refresh'), 'Fetch from Providers');

  const addModelBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    onclick: addModel
  }, icon('plus'), 'Add Model');

  // ── Model Groups Controls ──
  const groupSearchInput = h('input', {
    class: 'input',
    type: 'text',
    placeholder: 'Search model groups...',
    spellcheck: 'false',
    style: { width: '220px' },
    oninput: (e) => {
      groupFilterText = e.target.value.toLowerCase().trim();
      renderGroupsTable();
    }
  });

  const addGroupBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    onclick: () => openGroupModal()
  }, icon('plus'), 'Create Model Group');

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Models & Groups'),
        subText
      ),
      actionsWrap
    ),
    tabsNav,
    contentWrap
  ));

  function switchTab(tab) {
    activeTab = tab;
    tabModelsBtn.classList.toggle('active', tab === 'models');
    tabGroupsBtn.classList.toggle('active', tab === 'groups');

    if (tab === 'models') {
      subText.textContent = 'Control model firewall rules and access. Models are grouped by provider prefix (e.g. openrouter/model-id).';
      actionsWrap.replaceChildren(searchInput, providerSelect, fetchBtn, addModelBtn);
      renderModelsTable();
    } else {
      subText.textContent = 'Create reusable groups of models (e.g. GPT Ecosystem, Claude) that API keys dynamically link to.';
      actionsWrap.replaceChildren(groupSearchInput, addGroupBtn);
      renderGroupsTable();
    }
  }

  // ── All Models Tab Logic ──
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
      renderModelsTable();
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

  function renderModelsTable() {
    const tableCard = h('div', { class: 'card table-card' });

    let filtered = allModels.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(filterText) || (m.name && m.name.toLowerCase().includes(filterText));
      if (!matchesSearch) return false;
      if (!selectedProvider) return true;
      return m.provider_id === selectedProvider || m.id.startsWith(selectedProvider + '/');
    });

    if (!filtered.length) {
      const emptyAction = (filterText || selectedProvider)
        ? null
        : h('div', { style: { display: 'flex', gap: '8px', marginTop: '12px' } },
            h('button', { class: 'btn btn-primary', type: 'button', onclick: fetchFromProviders }, icon('refresh'), 'Fetch from Providers'),
            h('button', { class: 'btn', type: 'button', onclick: addModel }, icon('plus'), 'Add Model')
          );

      tableCard.replaceChildren(
        emptyState(
          'box',
          'No Models Found',
          filterText || selectedProvider
            ? 'No models match your current filter.'
            : 'No models configured yet. Click "Fetch from Providers" to retrieve models from upstream or "Add Model" to register one manually.',
          emptyAction
        )
      );
      contentWrap.replaceChildren(tableCard);
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
      renderModelsTable();
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
        renderModelsTable();
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

    contentWrap.replaceChildren(tableCard);
  }

  // ── Model Groups Tab Logic ──
  function renderGroupsTable() {
    const card = h('div', { class: 'card table-card' });

    let filtered = modelGroups.filter(g => {
      const q = groupFilterText;
      if (!q) return true;
      if (g.name && g.name.toLowerCase().includes(q)) return true;
      if (g.description && g.description.toLowerCase().includes(q)) return true;
      if (Array.isArray(g.models) && g.models.some(m => m.toLowerCase().includes(q))) return true;
      return false;
    });

    if (!filtered.length) {
      card.replaceChildren(
        emptyState('sparkles', 'No model groups found', groupFilterText ? 'No group matches your search query.' : 'Create a model group to easily assign sets of models to agent API keys.')
      );
      contentWrap.replaceChildren(card);
      return;
    }

    const rows = filtered.map(g => {
      const modelsArr = Array.isArray(g.models) ? g.models : [];
      const count = modelsArr.length;

      // Badges preview
      const previewBadges = modelsArr.slice(0, 3).map(m =>
        h('span', {
          class: 'badge',
          style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', marginRight: '4px' }
        }, m)
      );

      if (count > 3) {
        previewBadges.push(
          h('span', {
            class: 'badge muted',
            style: { fontSize: '11px', cursor: 'help' },
            title: modelsArr.join('\n')
          }, `+${count - 3} more`)
        );
      }

      const keysBadge = g.keys_count > 0
        ? h('span', { class: 'badge ok', style: { display: 'inline-flex', alignItems: 'center', gap: '4px' } }, icon('key'), `${g.keys_count} key${g.keys_count === 1 ? '' : 's'}`)
        : h('span', { class: 'badge muted' }, '0 keys');

      const editBtn = h('button', {
        class: 'btn btn-sm',
        type: 'button',
        title: 'Edit Model Group',
        onclick: () => openGroupModal(g)
      }, icon('pencil'), 'Edit');

      const deleteBtn = h('button', {
        class: 'btn btn-sm btn-danger',
        type: 'button',
        title: 'Delete Model Group',
        onclick: () => confirmDeleteGroup(g)
      }, icon('trash'));

      return h('tr', null,
        h('td', null,
          h('div', { class: 'strong', style: { display: 'flex', alignItems: 'center', gap: '6px' } },
            icon('sparkles'),
            g.name
          ),
          g.description ? h('div', { class: 'sub', style: { marginTop: '2px' } }, g.description) : null
        ),
        h('td', null,
          h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...previewBadges)
        ),
        h('td', { class: 'num' },
          h('span', { class: 'badge', style: { fontWeight: '600' } }, `${count} model${count === 1 ? '' : 's'}`)
        ),
        h('td', { style: { textAlign: 'center' } }, keysBadge),
        h('td', { class: 'muted', style: { fontSize: '11.5px' } }, g.updated_at ? fmtAgo(Date.parse(g.updated_at)) : '-'),
        h('td', { class: 'num' },
          h('div', { style: { display: 'inline-flex', gap: '6px' } }, editBtn, deleteBtn)
        )
      );
    });

    card.replaceChildren(
      h('div', { class: 'table-wrap' },
        h('table', { class: 'table' },
          h('thead', null,
            h('tr', null,
              h('th', { style: { width: '28%' } }, 'Group Name & Description'),
              h('th', null, 'Included Models'),
              h('th', { class: 'num', style: { width: '12%' } }, 'Models Count'),
              h('th', { style: { textAlign: 'center', width: '12%' } }, 'Linked API Keys'),
              h('th', { style: { width: '12%' } }, 'Last Updated'),
              h('th', { class: 'num', style: { width: '14%' } }, 'Action')
            )
          ),
          h('tbody', null, ...rows)
        )
      )
    );

    contentWrap.replaceChildren(card);
  }

  function openGroupModal(existingGroup = null) {
    const isEdit = Boolean(existingGroup);
    const existingModels = (existingGroup && Array.isArray(existingGroup.models)) ? existingGroup.models : [];

    const nameInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. GPT Ecosystem, Claude Family, Coding Specialists',
      value: existingGroup ? existingGroup.name : ''
    });

    const descInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. Curated models for high-performance agent tasks',
      value: existingGroup ? existingGroup.description : ''
    });

    const filterInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'Filter available models...',
      style: { fontSize: '12px', padding: '6px 10px' }
    });

    const availableModelIDs = new Set();
    allModels.forEach(m => {
      if (m && m.id) availableModelIDs.add(m.id);
    });

    const initialSelected = new Set();
    const leftoverCustom = [];
    existingModels.forEach(m => {
      if (availableModelIDs.has(m)) {
        initialSelected.add(m);
      } else {
        leftoverCustom.push(m);
      }
    });

    const selectedCount = h('span', { class: 'muted', style: { fontSize: '11.5px', marginLeft: 'auto' } });

    const checkboxes = new Map();
    const listItems = [];

    allModels.forEach(m => {
      const cb = h('input', {
        type: 'checkbox',
        checked: initialSelected.has(m.id)
      });
      checkboxes.set(m.id, cb);
      cb.onchange = updateCount;

      const item = h('label', {
        style: {
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '5px 8px',
          borderRadius: '4px',
          cursor: 'pointer',
          fontSize: '12px',
          fontFamily: 'monospace',
          userSelect: 'none'
        },
        onmouseenter: (e) => e.currentTarget.style.background = 'var(--hover)',
        onmouseleave: (e) => e.currentTarget.style.background = 'transparent'
      },
        cb,
        h('span', { style: { flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } }, m.id),
        m.provider_id ? h('span', { class: 'badge', style: { fontSize: '10px' } }, m.provider_id) : null
      );

      listItems.push({ id: m.id.toLowerCase(), node: item });
    });

    function updateCount() {
      let count = 0;
      checkboxes.forEach(cb => { if (cb.checked) count++; });
      selectedCount.textContent = `${count} model${count === 1 ? '' : 's'} selected`;
    }
    updateCount();

    filterInput.oninput = (e) => {
      const q = e.target.value.toLowerCase().trim();
      listItems.forEach(({ id, node }) => {
        node.style.display = (!q || id.includes(q)) ? 'flex' : 'none';
      });
    };

    const selectAllBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      onclick: () => {
        checkboxes.forEach(cb => { cb.checked = true; });
        updateCount();
      }
    }, 'Select All');

    const clearAllBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      onclick: () => {
        checkboxes.forEach(cb => { cb.checked = false; });
        updateCount();
      }
    }, 'Clear Selection');

    const actionsRow = h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
      selectAllBtn,
      clearAllBtn,
      selectedCount
    );

    const scrollList = h('div', {
      style: {
        maxHeight: '180px',
        overflowY: 'auto',
        border: '1px solid var(--border)',
        borderRadius: '6px',
        padding: '4px',
        background: 'var(--panel)'
      }
    },
      listItems.length > 0
        ? listItems.map(i => i.node)
        : h('div', { class: 'muted', style: { padding: '8px', fontSize: '12px', textAlign: 'center' } }, 'No models synced yet')
    );

    const customInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. ollama/*, gpt-4o, claude-3-5-sonnet',
      value: leftoverCustom.join(', '),
      style: { fontSize: '12px' }
    });

    const modelPickerWrap = h('div', {
      style: {
        display: 'flex',
        flexDirection: 'column',
        gap: '8px',
        padding: '12px',
        borderRadius: '8px',
        border: '1px solid var(--border)',
        background: 'var(--hover)'
      }
    },
      actionsRow,
      filterInput,
      scrollList,
      h('div', { style: { marginTop: '4px' } },
        h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', marginBottom: '4px' } }, 'Additional model IDs or wildcards (comma-separated):'),
        customInput
      )
    );

    formDialog({
      title: isEdit ? `Edit Model Group: ${existingGroup.name}` : 'Create New Model Group',
      submitText: isEdit ? 'Save Group' : 'Create Group',
      wide: true,
      extraWide: true,
      fields: [
        {
          label: 'Group Name',
          node: h('div', null,
            nameInput,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '3px 0 0' } }, 'A friendly name for this group (e.g. GPT Ecosystem, Claude, Coding Specialists).')
          )
        },
        {
          label: 'Description (Optional)',
          node: h('div', null,
            descInput,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '3px 0 0' } }, 'Short description of what this model group is meant for.')
          )
        },
        {
          label: 'Select Models in this Group',
          node: modelPickerWrap
        }
      ],
      onSubmit: async () => {
        const name = nameInput.value.trim();
        if (!name) throw new Error('Group name is required');
        const description = descInput.value.trim();

        const selected = [];
        checkboxes.forEach((cb, id) => {
          if (cb.checked) selected.push(id);
        });

        const extra = customInput.value.split(',').map(s => s.trim()).filter(Boolean);
        extra.forEach(m => {
          if (!selected.includes(m)) selected.push(m);
        });

        if (selected.length === 0) {
          throw new Error('Please select at least one model for this group');
        }

        try {
          if (isEdit) {
            await api.put(`/model-groups/${existingGroup.id}`, { name, description, models: selected });
            toast(`Model group "${name}" updated!`, 'ok');
          } else {
            await api.post('/model-groups', { name, description, models: selected });
            toast(`Model group "${name}" created!`, 'ok');
          }
          await load();
        } catch (e) {
          throw new Error(e.message || 'Operation failed');
        }
      }
    });
  }

  async function confirmDeleteGroup(g) {
    if (g.keys_count > 0) {
      toast(`Cannot delete group: it is currently linked to ${g.keys_count} API key(s). Please unlink it from those keys first.`, 'error');
      return;
    }

    const ok = await confirmDialog({
      title: `Delete Model Group "${g.name}"?`,
      body: 'Are you sure you want to delete this group? This action cannot be undone.',
      confirmText: 'Delete Group',
      danger: true,
    });
    if (!ok) return;

    try {
      await api.del(`/model-groups/${g.id}`);
      toast(`Model group "${g.name}" deleted`, 'ok');
      await load();
    } catch (e) {
      toast(`Delete failed: ${e.message}`, 'error');
    }
  }

  async function load() {
    contentWrap.replaceChildren(h('div', { class: 'skel', style: { height: '240px' } }));
    try {
      const [mRes, gRes, pRes] = await Promise.all([
        api.get('/models').catch((err) => {
          toast(`Failed to load models: ${err.message || 'Server error'}`, 'error');
          return [];
        }),
        api.get('/model-groups').catch(() => ({ groups: [] })),
        api.get('/providers').catch((err) => {
          toast(`Failed to load providers: ${err.message || 'Server error'}`, 'error');
          return { providers: [] };
        })
      ]);
      allModels = Array.isArray(mRes) ? mRes : [];
      modelGroups = (gRes && Array.isArray(gRes.groups)) ? gRes.groups : [];
      providersList = (pRes && Array.isArray(pRes.providers)) ? pRes.providers : [];

      tabModelsBadge.textContent = fmtNum(allModels.length);
      tabGroupsBadge.textContent = fmtNum(modelGroups.length);

      providerSelect.setOptions(
        providersList.map(p => ({
          value: p.prefix || p.id,
          label: p.name ? `${p.name} (${p.prefix || 'root'})` : (p.prefix || p.id),
          badge: p.prefix || 'root'
        })),
        selectedProvider,
        'All Providers'
      );
    } catch (e) {
      if (alive) {
        toast(`Error loading models: ${e.message}`, 'error');
        contentWrap.replaceChildren(
          emptyState('alert', 'Could Not Load Models', e.message,
            h('button', { class: 'btn btn-sm', type: 'button', style: { marginTop: '10px' }, onclick: load }, icon('refresh'), 'Retry')
          )
        );
      }
      return;
    }

    if (!alive) return;
    if (activeTab === 'models') {
      actionsWrap.replaceChildren(searchInput, providerSelect, fetchBtn, addModelBtn);
      renderModelsTable();
    } else {
      actionsWrap.replaceChildren(groupSearchInput, addGroupBtn);
      renderGroupsTable();
    }
  }

  load();
  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
