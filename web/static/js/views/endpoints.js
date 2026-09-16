// Endpoints & Agent Setup: Upstream provider configuration, NineGuard API key generation, and IDE guides.
import { api } from '../api.js';
import { h, icon, toast, fmtNum, fmtCompact, emptyState, formDialog, confirmDialog, searchableSelect } from '../ui.js';
import { setRoute } from '../state.js';

export function mount(root) {
  let alive = true;
  let activeTab = 'cursor';
  let modelsList = [];
  let keysList = [];
  let modelGroupsList = [];
  let upstreamInfo = { configured: false, masked_key: '', key: '', router_target: '' };

  const origin = location.origin || `${location.protocol}//${location.host}` || 'http://localhost:8080';
  const baseUrl = `${origin}/v1`;

  const upstreamCard = h('div', { class: 'card mt' });
  const keysCard = h('div', { class: 'card mt' });
  const guidesWrap = h('div', { class: 'card-body p-0' });
  const testerCard = h('div', { class: 'card mt' });

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Endpoints & Agent Setup'),
      h('p', null, 'Manage upstream provider connection, generate NineGuard keys for your agents, and configure tools')
    )
  );

  // ── Copy Helper ──
  function copyText(text, label = 'Copied to clipboard!') {
    navigator.clipboard.writeText(text).then(() => {
      toast(label, 'ok');
    }).catch(() => {
      toast('Failed to copy', 'error');
    });
  }

  function copyField(label, val) {
    return h('div', { class: 'copy-box', style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', background: 'var(--hover)', borderRadius: '6px', marginBottom: '8px' } },
      h('div', { style: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } },
        h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase', letterSpacing: '0.05em' } }, label),
        h('code', { style: { fontSize: '13px', fontWeight: '600', color: 'var(--accent)' } }, val)
      ),
      h('button', {
        class: 'btn btn-sm',
        type: 'button',
        title: 'Copy to clipboard',
        onclick: () => copyText(val, `${label} copied!`)
      }, icon('copy'), 'Copy')
    );
  }

  // ── Flow Architecture Card ──
  function renderFlowCard() {
    return h('div', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Architecture: NineGuard as Master Proxy for Agents'),
          h('p', { class: 'card-sub' }, 'Agents connect to NineGuard using their own keys; NineGuard authorizes calls and forwards to upstream providers')
        )
      ),
      h('div', { class: 'flow-wrap', style: { padding: '14px', display: 'flex', flexDirection: 'column', gap: '12px' } },
        h('div', { class: 'flow-steps', style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '12px' } },
          // Step 1: Agent
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('user'), h('b', null, '1. AI Agent (Client)')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Cursor, Cline, Continue use Base URL ', h('code', null, ':8080/v1'), ' with NineGuard key (', h('code', null, 'sk-ng-...'), ').'
            )
          ),
          // Step 2: NineGuard
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1.5px solid var(--accent)', background: 'var(--hover)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px', color: 'var(--accent)' } },
              icon('shield'), h('b', null, '2. NineGuard (:8080)')
            ),
            h('p', { style: { fontSize: '12px', margin: 0 } },
              'Validates agent key, enforces model firewall (403), tracks tokens, and injects Upstream Master Key.'
            )
          ),
          // Step 3: Upstream Provider
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('server'), h('b', null, '3. Upstream Provider')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Receives request authenticated with Upstream Master Key and handles model provider load balancing.'
            )
          ),
          // Step 4: Upstream LLMs
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('sparkles'), h('b', null, '4. AI Model Providers')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Claude, OpenAI, Gemini, DeepSeek, or local Ollama LLMs.'
            )
          )
        )
      )
    );
  }

  // ── Upstream Provider Setup Card ──
  function renderUpstreamCard() {
    const isConfigured = upstreamInfo && upstreamInfo.configured && upstreamInfo.key;
    const targetUrl = upstreamInfo?.router_target || '';

    const statusBadge = isConfigured
      ? h('span', { class: 'badge ok', style: { padding: '4px 8px' } }, icon('check'), ' Upstream Key Configured')
      : h('span', { class: 'badge muted', style: { padding: '4px 8px' } }, icon('server'), ' Multi-Provider Routing');

    upstreamCard.replaceChildren(
      h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
        h('div', null,
          h('h2', null, 'Upstream AI Providers'),
          h('p', { class: 'card-sub' }, 'NineGuard relays all authorized agent requests to configured OpenAI-compatible upstream providers')
        ),
        statusBadge
      ),
      h('div', { style: { padding: '14px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px', background: 'var(--hover)', borderRadius: '6px' } },
        h('div', null,
          h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase' } }, 'Default Upstream Route'),
          targetUrl
            ? h('code', { style: { fontWeight: 'bold' } }, targetUrl)
            : h('span', { class: 'muted', style: { fontStyle: 'italic', fontSize: '12px' } }, 'None configured (Use Providers menu to register upstream endpoints)'),
          isConfigured
            ? h('span', { class: 'muted', style: { marginLeft: '12px', fontSize: '12px' } }, 'Master Key: ', h('code', null, upstreamInfo.masked_key))
            : null
        ),
        h('button', {
          class: 'btn btn-sm btn-primary',
          type: 'button',
          onclick: () => setRoute('providers')
        }, icon('server'), 'Manage Providers')
      )
    );
  }

  // ── NineGuard Client API Keys Card ──
  function renderKeysCard() {
    const head = h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
      h('div', null,
        h('h2', null, 'NineGuard Client API Keys'),
        h('p', { class: 'card-sub' }, 'Generate dedicated keys for Cursor, Cline, Pi, and developers. Every token and request is tracked per key.')
      ),
      h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: () => openKeyModal() }, icon('plus'), 'Generate New Key')
    );

    if (!keysList.length) {
      keysCard.replaceChildren(head, emptyState('key', 'No NineGuard API keys generated yet', 'Click "Generate New Key" to issue your first API key for an agent.'));
      return;
    }

    const table = h('table', { class: 'table' },
      h('thead', null,
        h('tr', null,
          h('th', null, 'Agent / Key Name'),
          h('th', null, 'Key Token'),
          h('th', null, 'Allowed Models'),
          h('th', { class: 'num' }, 'Requests'),
          h('th', { class: 'num' }, 'Tokens'),
          h('th', null, 'Status'),
          h('th', { style: { textAlign: 'right' } }, 'Actions')
        )
      ),
      h('tbody', null,
        ...keysList.map(k => {
          const raw = k.raw_key || k.key;
          const statusBtn = h('button', {
            class: `badge ${k.is_active ? 'ok' : 'muted'} badge-btn`,
            type: 'button',
            onclick: async () => {
              try {
                await api.post(`/keys/${k.id}/toggle`, { active: !k.is_active });
                toast(`Key ${k.is_active ? 'deactivated' : 'activated'}`, 'ok');
                await load();
              } catch (e) {
                toast(`Failed: ${e.message}`, 'error');
              }
            }
          }, k.is_active ? 'Active' : 'Disabled');

          // Allowed Models / Groups Display
          const keyMode = k.model_access_mode || (
            (!k.allowed_models || k.allowed_models.length === 0 || k.allowed_models.includes('*') || k.allowed_models.includes('all'))
              ? 'all'
              : 'custom'
          );

          let allowedCell;
          if (keyMode === 'all') {
            allowedCell = h('td', null,
              h('span', { class: 'badge ok', style: { fontSize: '11px', display: 'inline-flex', alignItems: 'center', gap: '4px' } },
                icon('check'), 'All Models (*)'
              )
            );
          } else if (keyMode === 'group') {
            const gids = Array.isArray(k.model_group_ids) ? k.model_group_ids : [];
            const groupBadges = gids.map(gid => {
              const grp = modelGroupsList.find(g => g.id === gid);
              const label = grp ? grp.name : gid;
              const modelPreview = grp && grp.models ? grp.models.join(', ') : '';
              return h('span', {
                class: 'badge',
                style: { background: 'var(--accent)', color: '#fff', fontSize: '11px', marginRight: '4px', cursor: 'help', display: 'inline-flex', alignItems: 'center', gap: '4px' },
                title: modelPreview ? `Models in ${label}:\n${grp.models.join('\n')}` : label
              }, icon('sparkles'), label);
            });
            if (groupBadges.length === 0) {
              groupBadges.push(h('span', { class: 'badge err', style: { fontSize: '11px' } }, 'No groups linked'));
            }
            allowedCell = h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...groupBadges));
          } else {
            const allowedList = Array.isArray(k.allowed_models) ? k.allowed_models : [];
            const count = allowedList.length;
            const badges = allowedList.slice(0, 2).map(m =>
              h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', marginRight: '4px' } }, m)
            );
            if (count > 2) {
              badges.push(
                h('span', {
                  class: 'badge muted',
                  style: { fontSize: '11px', cursor: 'help' },
                  title: allowedList.join('\n')
                }, `+${count - 2} more`)
              );
            }
            allowedCell = h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...badges));
          }

          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '8px' } }, icon('key')),
              k.name
            ),
            h('td', null, h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)),
            allowedCell,
            h('td', { class: 'num' }, fmtNum(k.total_requests || 0)),
            h('td', { class: 'num' }, fmtCompact(k.total_tokens || 0)),
            h('td', null, statusBtn),
            h('td', { style: { textAlign: 'right' } },
              h('div', { style: { display: 'inline-flex', gap: '6px' } },
                h('button', {
                  class: 'btn btn-sm',
                  type: 'button',
                  title: 'Edit Key & Model Access',
                  onclick: () => openKeyModal(k)
                }, icon('pencil'), 'Edit'),
                h('button', {
                  class: 'btn btn-sm',
                  type: 'button',
                  title: 'Copy NineGuard API Key',
                  onclick: () => copyText(raw, `Key "${k.name}" copied!`)
                }, icon('copy'), 'Copy Key'),
                h('button', {
                  class: 'btn btn-sm btn-danger',
                  type: 'button',
                  title: 'Delete Key',
                  onclick: () => confirmDeleteKey(k)
                }, icon('trash'))
              )
            )
          );
        })
      )
    );

    keysCard.replaceChildren(head, h('div', { class: 'table-wrap' }, table));
  }

  function openKeyModal(existingKey = null) {
    const isEdit = Boolean(existingKey);
    const existingModels = (existingKey && Array.isArray(existingKey.allowed_models)) ? existingKey.allowed_models : [];
    const existingGroupIDs = new Set((existingKey && Array.isArray(existingKey.model_group_ids)) ? existingKey.model_group_ids : []);

    let initialMode = 'all';
    if (existingKey) {
      if (existingKey.model_access_mode) {
        initialMode = existingKey.model_access_mode;
      } else if (existingModels.length > 0 && !existingModels.includes('*') && !existingModels.includes('all')) {
        initialMode = 'custom';
      }
    }

    const nameInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. Cursor IDE, Cline Mac, Pi Agent',
      value: existingKey ? existingKey.name : 'Cursor IDE'
    });

    let mode = initialMode;

    // ── Segmented Mode Buttons ──
    const tabBtnStyle = {
      flex: '1 1 0',
      minWidth: '0',
      display: 'inline-flex',
      alignItems: 'center',
      justifyContent: 'center',
      gap: '6px',
      fontSize: '12px',
      height: '32px',
      padding: '0 10px',
      whiteSpace: 'nowrap',
      boxSizing: 'border-box',
      cursor: 'pointer',
      textAlign: 'center'
    };

    const btnAll = h('button', {
      type: 'button',
      class: `tab${mode === 'all' ? ' active' : ''}`,
      style: { ...tabBtnStyle },
      onclick: () => switchAccessMode('all')
    }, icon('check'), 'All Models (Global)');

    const btnGroup = h('button', {
      type: 'button',
      class: `tab${mode === 'group' ? ' active' : ''}`,
      style: { ...tabBtnStyle },
      onclick: () => switchAccessMode('group')
    }, icon('sparkles'), 'Model Groups');

    const btnCustom = h('button', {
      type: 'button',
      class: `tab${mode === 'custom' ? ' active' : ''}`,
      style: { ...tabBtnStyle },
      onclick: () => switchAccessMode('custom')
    }, icon('pencil'), 'Custom Selection');

    const modeSelector = h('div', {
      class: 'tabs',
      style: {
        width: '100%',
        boxSizing: 'border-box',
        marginBottom: '10px',
        display: 'flex',
        gap: '4px',
        padding: '3px'
      }
    },
      btnAll, btnGroup, btnCustom
    );

    // ── Panel 1: All Models ──
    const panelAll = h('div', {
      style: {
        display: mode === 'all' ? 'block' : 'none',
        padding: '12px 14px',
        background: 'var(--hover)',
        borderRadius: '8px',
        border: '1px solid var(--border)'
      }
    },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', fontWeight: '600', marginBottom: '4px' } },
        icon('check'), 'Inherit Global Firewall Configuration'
      ),
      h('p', { class: 'muted', style: { fontSize: '12px', margin: 0, lineHeight: 1.4 } },
        'Memberi akses ke semua model yang aktif di global settings. Model baru yang ditambahkan di masa depan otomatis langsung bisa diakses tanpa perlu mengubah settingan API key ini.'
      )
    );

    // ── Panel 2: Model Groups ──
    const groupCheckboxes = new Map();
    const groupListItems = [];

    const previewContainer = h('div', {
      style: {
        marginTop: '10px',
        padding: '10px 12px',
        background: 'var(--panel)',
        borderRadius: '6px',
        border: '1px solid var(--border)'
      }
    });

    function updateGroupPreview() {
      const selectedGroups = [];
      const unionModels = new Set();
      modelGroupsList.forEach(g => {
        const cb = groupCheckboxes.get(g.id);
        if (cb && cb.checked) {
          selectedGroups.push(g);
          (g.models || []).forEach(m => unionModels.add(m));
        }
      });

      if (selectedGroups.length === 0) {
        previewContainer.replaceChildren(
          h('div', { class: 'muted', style: { fontSize: '12px', fontStyle: 'italic' } },
            'Belum ada group yang dipilih. Silakan centang minimal satu model group di atas.'
          )
        );
        return;
      }

      const badges = Array.from(unionModels).map(m =>
        h('span', {
          class: 'badge',
          style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', margin: '2px' }
        }, m)
      );

      previewContainer.replaceChildren(
        h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' } },
          h('b', { style: { fontSize: '12px' } }, `Preview Model yang Diizinkan (${unionModels.size} models):`),
          h('span', { class: 'badge ok', style: { fontSize: '10px', display: 'inline-flex', alignItems: 'center', gap: '3px' } }, icon('sparkles'), 'Dynamic Sync')
        ),
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '4px', maxHeight: '100px', overflowY: 'auto' } }, ...badges),
        h('p', { class: 'muted', style: { fontSize: '11px', margin: '8px 0 0', lineHeight: 1.3 } },
          '💡 Dynamic Reference: Jika ada model baru ditambahkan ke group di atas nanti, API key ini otomatis langsung mendapatkan akses tanpa perlu disentuh ulang.'
        )
      );
    }

    if (modelGroupsList.length === 0) {
      groupListItems.push(
        h('div', { class: 'muted', style: { padding: '12px', textAlign: 'center', fontSize: '12px' } },
          'Belum ada Model Group. Buat group di menu Models > Model Groups.'
        )
      );
    } else {
      modelGroupsList.forEach(g => {
        const cb = h('input', {
          type: 'checkbox',
          checked: existingGroupIDs.has(g.id)
        });
        groupCheckboxes.set(g.id, cb);
        cb.onchange = updateGroupPreview;

        const count = Array.isArray(g.models) ? g.models.length : 0;
        const item = h('label', {
          style: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '8px 10px',
            borderRadius: '6px',
            cursor: 'pointer',
            fontSize: '12.5px',
            background: 'var(--panel)',
            border: '1px solid var(--border)',
            userSelect: 'none'
          },
          onmouseenter: (e) => e.currentTarget.style.borderColor = 'var(--accent)',
          onmouseleave: (e) => e.currentTarget.style.borderColor = 'var(--border)'
        },
          cb,
          h('div', { style: { flex: 1, minWidth: 0 } },
            h('div', { style: { fontWeight: '600', display: 'flex', alignItems: 'center', gap: '6px' } },
              icon('sparkles'),
              g.name
            ),
            g.description ? h('div', { class: 'muted', style: { fontSize: '11px', marginTop: '1px' } }, g.description) : null
          ),
          h('span', { class: 'badge', style: { fontSize: '11px', whiteSpace: 'nowrap' } }, `${count} model${count === 1 ? '' : 's'}`)
        );
        groupListItems.push(item);
      });
    }

    const panelGroup = h('div', {
      style: {
        display: mode === 'group' ? 'flex' : 'none',
        flexDirection: 'column',
        gap: '8px',
        padding: '12px',
        borderRadius: '8px',
        border: '1px solid var(--border)',
        background: 'var(--hover)'
      }
    },
      h('div', { style: { fontSize: '12px', fontWeight: '500', marginBottom: '2px' } }, 'Select Model Group(s):'),
      h('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px', maxHeight: '170px', overflowY: 'auto' } }, ...groupListItems),
      previewContainer
    );
    updateGroupPreview();

    // ── Panel 3: Custom Selection ──
    const filterInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'Filter available models...',
      style: { fontSize: '12px', padding: '6px 10px' }
    });

    const availableModelIDs = new Set();
    modelsList.forEach(m => {
      if (m && m.id) availableModelIDs.add(m.id);
    });

    const initialSelected = new Set();
    const leftoverCustom = [];
    if (initialMode === 'custom') {
      existingModels.forEach(m => {
        if (m === '*' || m === 'all') return;
        if (availableModelIDs.has(m)) {
          initialSelected.add(m);
        } else {
          leftoverCustom.push(m);
        }
      });
    }

    const selectedCount = h('span', { class: 'muted', style: { fontSize: '11.5px', marginLeft: 'auto' } });

    const checkboxes = new Map();
    const listItems = [];

    modelsList.forEach(m => {
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
        onmouseenter: (e) => e.currentTarget.style.background = 'var(--panel)',
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

    // Detour helper: Load from Group dropdown
    const detourSelect = searchableSelect({
      placeholder: '⚡ Customize from Group...',
      searchPlaceholder: 'Search model group...',
      clearable: false,
      compact: true,
      style: { fontSize: '11.5px', height: '28px', maxWidth: '210px' },
      options: [
        { value: '', label: '⚡ Customize from Group...' },
        ...modelGroupsList.map(g => ({
          value: g.id,
          label: `${g.name} (${(g.models || []).length})`,
          badge: `${(g.models || []).length} models`
        }))
      ],
      onChange: (gid) => {
        if (!gid) return;
        const grp = modelGroupsList.find(g => g.id === gid);
        if (grp && Array.isArray(grp.models)) {
          let added = 0;
          grp.models.forEach(m => {
            const cb = checkboxes.get(m);
            if (cb) {
              cb.checked = true;
              added++;
            }
          });
          updateCount();
          toast(`Checked ${added} models from group "${grp.name}"`, 'ok');
        }
        detourSelect.setValue('');
      }
    });

    const actionsRow = h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
      detourSelect,
      selectAllBtn,
      clearAllBtn,
      selectedCount
    );

    const scrollList = h('div', {
      style: {
        maxHeight: '160px',
        overflowY: 'auto',
        border: '1px solid var(--border)',
        borderRadius: '6px',
        padding: '4px',
        background: 'var(--panel)'
      }
    },
      listItems.length > 0
        ? listItems.map(i => i.node)
        : h('div', { class: 'muted', style: { padding: '8px', fontSize: '12px', textAlign: 'center' } }, 'No models synced yet in NineGuard')
    );

    const customInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. openrouter/*, gpt-4o, claude-3-5-sonnet',
      value: leftoverCustom.join(', '),
      style: { fontSize: '12px' }
    });

    const panelCustom = h('div', {
      style: {
        display: mode === 'custom' ? 'flex' : 'none',
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

    function switchAccessMode(newMode) {
      mode = newMode;
      btnAll.classList.toggle('active', mode === 'all');
      btnGroup.classList.toggle('active', mode === 'group');
      btnCustom.classList.toggle('active', mode === 'custom');

      panelAll.style.display = mode === 'all' ? 'block' : 'none';
      panelGroup.style.display = mode === 'group' ? 'flex' : 'none';
      panelCustom.style.display = mode === 'custom' ? 'flex' : 'none';
    }

    formDialog({
      title: isEdit ? `Edit API Key: ${existingKey.name}` : 'Generate New NineGuard API Key',
      submitText: isEdit ? 'Save Changes' : 'Generate Key',
      wide: true,
      extraWide: true,
      fields: [
        {
          label: 'Key Name / Description',
          node: h('div', null,
            nameInput,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '3px 0 0' } }, 'Name for tracking usage in traffic logs.')
          )
        },
        {
          label: 'Allowed Models (Access Control)',
          node: h('div', null,
            modeSelector,
            panelAll,
            panelGroup,
            panelCustom,
            h('p', { class: 'muted', style: { fontSize: '11.5px', marginTop: '6px' } },
              'Configure which models this API key can call. Unauthorized model calls are blocked with HTTP 403 Forbidden.'
            )
          )
        }
      ],
      onSubmit: async () => {
        const name = nameInput.value.trim() || (isEdit ? existingKey.name : 'Agent Key');
        let model_access_mode = mode;
        let model_group_ids = [];
        let allowed_models = [];

        if (mode === 'group') {
          groupCheckboxes.forEach((cb, gid) => {
            if (cb.checked) model_group_ids.push(gid);
          });
          if (model_group_ids.length === 0) {
            throw new Error('Please select at least one Model Group or switch to "All Models".');
          }
        } else if (mode === 'custom') {
          const selected = [];
          checkboxes.forEach((cb, id) => {
            if (cb.checked) selected.push(id);
          });

          const extra = customInput.value.split(',').map(s => s.trim()).filter(Boolean);
          extra.forEach(m => {
            if (!selected.includes(m)) selected.push(m);
          });

          if (selected.length === 0) {
            throw new Error('Please select at least one model or switch to "All Models (*)".');
          }
          allowed_models = selected;
        }

        const payload = {
          name,
          model_access_mode,
          model_group_ids,
          allowed_models
        };

        try {
          if (isEdit) {
            await api.put(`/keys/${existingKey.id}`, payload);
            toast(`Key "${name}" updated!`, 'ok');
            await load();
          } else {
            const newKey = await api.post('/keys', payload);
            toast(`Key "${name}" created!`, 'ok');
            await load();
            showGeneratedKeyModal(newKey);
          }
        } catch (e) {
          throw new Error(e.message || 'Operation failed');
        }
      }
    });
  }

  function showGeneratedKeyModal(newKey) {
    const raw = newKey.raw_key || newKey.key;
    formDialog({
      title: 'NineGuard API Key Generated',
      submitText: 'Done',
      cancel: false,
      fields: [
        { node: h('div', { class: 'note ok mt-sm' }, icon('check'), h('b', null, 'Your API key is ready:')) },
        { node: h('div', { style: { padding: '14px', background: 'var(--hover)', borderRadius: '6px', margin: '10px 0' } },
          h('div', { class: 'muted', style: { fontSize: '11px', marginBottom: '4px' } }, `Name: ${newKey.name}`),
          h('code', { style: { fontSize: '14px', fontWeight: 'bold', color: 'var(--accent)', wordBreak: 'break-all' } }, raw)
        )},
        { node: h('button', {
          class: 'btn btn-primary btn-block',
          type: 'button',
          onclick: () => copyText(raw, 'API Key copied!')
        }, icon('copy'), 'Copy API Key') }
      ]
    });
  }

  async function confirmDeleteKey(k) {
    const ok = await confirmDialog({
      title: `Delete Key "${k.name}"?`,
      body: `Agents using this key (${k.key}) will immediately receive HTTP 401 Unauthorized. Existing traffic logs will be preserved.`,
      confirmText: 'Delete Key',
      danger: true,
    });
    if (!ok) return;

    try {
      await api.del(`/keys/${k.id}`);
      toast(`Key "${k.name}" deleted`, 'ok');
      await load();
    } catch (e) {
      toast(`Failed to delete key: ${e.message}`, 'error');
    }
  }

  // ── Integration Guides Card ──
  function renderGuides() {
    const activeKeyObj = keysList.find(k => k.is_active) || keysList[0];
    const defaultKey = activeKeyObj ? (activeKeyObj.raw_key || activeKeyObj.key) : 'sk-ng-YOUR_NINEGUARD_KEY';
    const sampleModel = modelsList.length > 0 ? modelsList[0].id : 'gpt-4o';

    const guides = {
      cursor: {
        title: 'Cursor IDE',
        desc: 'Configure custom OpenAI endpoint in Cursor Settings',
        steps: [
          'Open Cursor Settings → Models → OpenAI API Key.',
          'Toggle Override OpenAI Base URL to ON.',
          `Base URL: ${baseUrl}`,
          `API Key: ${defaultKey}`,
          'Add your models (e.g. gpt-4o, claude-3-5-sonnet).'
        ],
        code: `# Cursor IDE Configuration
OpenAI Base URL: ${baseUrl}
API Key: ${defaultKey}
Model: ${sampleModel}`
      },
      cline: {
        title: 'Cline / Roo Code (VS Code)',
        desc: 'Autonomous coding agent extension for VS Code',
        steps: [
          'Click the Cline extension icon in the VS Code sidebar.',
          'Open Settings (gear icon) → Select API Provider: "OpenAI Compatible".',
          `Base URL: ${baseUrl}`,
          `API Key: ${defaultKey}`,
          `Model ID: ${sampleModel}`
        ],
        code: `// Cline Provider Configuration
Provider: OpenAI Compatible
Base URL: ${baseUrl}
API Key: ${defaultKey}
Model ID: ${sampleModel}`
      },
      continue: {
        title: 'Continue.dev',
        desc: 'Open-source AI coding assistant for VS Code and JetBrains',
        steps: [
          'Open your ~/.continue/config.json file.',
          'Add a new model entry under "models" array as shown below.'
        ],
        code: `// ~/.continue/config.json
{
  "models": [
    {
      "title": "NineGuard (${sampleModel})",
      "provider": "openai",
      "model": "${sampleModel}",
      "apiBase": "${baseUrl}",
      "apiKey": "${defaultKey}"
    }
  ]
}`
      },
      pi: {
        title: 'Pi Agent Harness',
        desc: 'High-performance coding agent harness CLI',
        steps: [
          'Configure your environment variable or pass inline when launching.',
          'NineGuard will intercept, filter, and track token usage automatically.'
        ],
        code: `# Set environment variables for Pi Agent
export OPENAI_BASE_URL="${baseUrl}"
export OPENAI_API_KEY="${defaultKey}"

# Or execute Pi directly:
OPENAI_BASE_URL="${baseUrl}" OPENAI_API_KEY="${defaultKey}" pi`
      },
      python: {
        title: 'Python (OpenAI SDK)',
        desc: 'Official OpenAI Python client library',
        steps: [
          'Install package: pip install openai',
          'Instantiate OpenAI client pointing to NineGuard proxy.'
        ],
        code: `from openai import OpenAI

client = OpenAI(
    base_url="${baseUrl}",
    api_key="${defaultKey}"
)

response = client.chat.completions.create(
    model="${sampleModel}",
    messages=[{"role": "user", "content": "Hello from NineGuard!"}]
)
print(response.choices[0].message.content)`
      },
      node: {
        title: 'Node.js (OpenAI SDK)',
        desc: 'Official OpenAI Node.js / TypeScript SDK',
        steps: [
          'Install package: npm install openai',
          'Set baseURL to NineGuard proxy.'
        ],
        code: `import OpenAI from 'openai';

const openai = new OpenAI({
  baseURL: '${baseUrl}',
  apiKey: '${defaultKey}',
});

const response = await openai.chat.completions.create({
  model: '${sampleModel}',
  messages: [{ role: 'user', content: 'Hello from NineGuard!' }],
});
console.log(response.choices[0].message.content);`
      },
      curl: {
        title: 'cURL / Shell',
        desc: 'Direct HTTP terminal request test',
        steps: [
          'Run the command in your terminal to test chat completions.'
        ],
        code: `curl -X POST "${baseUrl}/chat/completions" \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${defaultKey}" \\
  -d '{
    "model": "${sampleModel}",
    "messages": [{"role": "user", "content": "Ping NineGuard"}]
  }'`
      }
    };

    const tabKeys = Object.keys(guides);
    const tabBtns = tabKeys.map(k => h('button', {
      class: `tab-btn${activeTab === k ? ' active' : ''}`,
      type: 'button',
      onclick: () => {
        activeTab = k;
        renderGuides();
      }
    }, guides[k].title));

    const g = guides[activeTab] || guides.cursor;

    guidesWrap.replaceChildren(
      h('div', { class: 'tab-nav' }, ...tabBtns),
      h('div', { style: { padding: '20px' } },
        h('h3', { style: { margin: '0 0 6px 0' } }, g.title),
        h('p', { class: 'muted', style: { fontSize: '13px', marginBottom: '14px' } }, g.desc),
        h('ol', { style: { paddingLeft: '18px', margin: '0 0 16px 0', fontSize: '13px', lineHeight: '1.6' } },
          ...g.steps.map(s => h('li', null, s))
        ),
        h('div', { style: { position: 'relative' } },
          h('pre', { class: 'code-block', style: { padding: '14px', borderRadius: '6px', background: 'var(--hover)', overflowX: 'auto', fontSize: '12px' } }, g.code),
          h('button', {
            class: 'btn btn-sm',
            style: { position: 'absolute', top: '8px', right: '8px' },
            type: 'button',
            onclick: () => copyText(g.code, `${g.title} snippet copied!`)
          }, icon('copy'), 'Copy Snippet')
        )
      )
    );
  }

  // ── Live Endpoint Tester ──
  function renderTester() {
    const enabledModels = modelsList.filter(m => m.enabled);
    const activeKeys = keysList.filter(k => k.is_active);

    const modelSelect = searchableSelect({
      placeholder: enabledModels.length ? 'Select model...' : 'No models available',
      searchPlaceholder: 'Search models...',
      ariaLabel: 'Target Model',
      wide: true,
      compact: true,
      style: { flex: '1', minWidth: '170px' },
      options: enabledModels.map(m => {
        const prov = m.provider_id || (m.id.includes('/') ? m.id.split('/')[0] : '');
        return {
          value: m.id,
          label: m.id,
          badge: prov || ''
        };
      }),
      value: enabledModels[0]?.id || ''
    });

    const keySelect = searchableSelect({
      placeholder: activeKeys.length ? 'Select API key...' : 'No keys available',
      searchPlaceholder: 'Search keys...',
      ariaLabel: 'NineGuard API Key',
      wide: true,
      compact: true,
      style: { flex: '1', minWidth: '170px' },
      options: activeKeys.map(k => ({
        value: k.raw_key || k.key,
        label: `${k.name} (${k.key})`,
        badge: k.is_active ? 'active' : ''
      })),
      value: activeKeys[0] ? (activeKeys[0].raw_key || activeKeys[0].key) : ''
    });

    const promptInput = h('input', {
      class: 'input',
      type: 'text',
      value: 'Hi NineGuard! Give me a 1-sentence response.',
      style: { flex: '2' }
    });

    const testBtn = h('button', { class: 'btn btn-primary', type: 'button' }, icon('sparkles'), 'Test Ping');
    const resultBox = h('div', { style: { display: 'none', marginTop: '14px', padding: '12px', background: 'var(--hover)', borderRadius: '6px' } });

    testBtn.onclick = async () => {
      const model = modelSelect.value;
      const key = keySelect.value;
      const prompt = promptInput.value.trim();

      if (!model) return toast('No model selected', 'warn');
      if (!key) return toast('No NineGuard API key selected', 'warn');

      testBtn.disabled = true;
      testBtn.textContent = 'Sending...';
      resultBox.style.display = 'block';
      resultBox.replaceChildren(h('span', { class: 'muted' }, 'Connecting to proxy /v1/chat/completions...'));

      const startTime = performance.now();
      try {
        const resp = await fetch('/v1/chat/completions', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${key}`,
          },
          body: JSON.stringify({
            model: model,
            messages: [{ role: 'user', content: prompt || 'ping' }],
            stream: false,
          })
        });

        const duration = Math.round(performance.now() - startTime);
        const data = await resp.json().catch(() => null);

        if (resp.ok) {
          const content = data?.choices?.[0]?.message?.content || '(Empty content)';
          const totalTok = data?.usage?.total_tokens || 0;

          resultBox.replaceChildren(
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
              h('span', { class: 'badge ok' }, `HTTP ${resp.status} OK`),
              h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms · Tokens: ${totalTok}`)
            ),
            h('div', { style: { fontSize: '13px', whiteSpace: 'pre-wrap', lineHeight: '1.5' } }, content)
          );
          toast('Proxy test succeeded!', 'ok');
        } else {
          const errText = data?.error?.message || `HTTP ${resp.status} ${resp.statusText}`;
          resultBox.replaceChildren(
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
              h('span', { class: 'badge err' }, `HTTP ${resp.status} Failed`),
              h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms`)
            ),
            h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, errText)
          );
          toast(`Test failed: ${errText}`, 'error');
        }
      } catch (err) {
        const duration = Math.round(performance.now() - startTime);
        resultBox.replaceChildren(
          h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, `Network error: ${err.message} (${duration}ms)`)
        );
        toast(`Network error: ${err.message}`, 'error');
      } finally {
        testBtn.disabled = false;
        testBtn.replaceChildren(icon('sparkles'), 'Test Ping');
      }
    };

    testerCard.replaceChildren(
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Live Endpoint Probe (Connection Test)'),
          h('p', { class: 'card-sub' }, 'Test the full pipeline Agent → NineGuard → Upstream Provider directly from your browser')
        )
      ),
      h('div', { style: { padding: '16px' } },
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' } },
          modelSelect,
          keySelect,
          promptInput,
          testBtn
        ),
        resultBox
      )
    );
  }

  // ── Endpoints URL Summary Box ──
  function renderUrlCard() {
    return h('div', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'OpenAI-Compatible URLs'),
          h('p', { class: 'card-sub' }, 'Configure these endpoints in your developer tools and coding agents')
        )
      ),
      h('div', { style: { padding: '16px' } },
        copyField('OpenAI Base URL (Recommended for agents)', baseUrl),
        copyField('Chat Completions Endpoint', `${baseUrl}/chat/completions`),
        copyField('Models List Endpoint', `${baseUrl}/models`),
        copyField('Health Probe Endpoint', `${origin}/healthz`)
      )
    );
  }

  // ── Main Load ──
  async function load() {
    try {
      const [u, m, k, g] = await Promise.all([
        api.get('/settings/upstream').catch(() => ({})),
        api.get('/models').catch(() => []),
        api.get('/keys').catch(() => ({ keys: [] })),
        api.get('/model-groups').catch(() => ({ groups: [] }))
      ]);
      upstreamInfo = u || {};
      modelsList = Array.isArray(m) ? m : [];
      keysList = (k && k.keys) ? k.keys : [];
      modelGroupsList = (g && Array.isArray(g.groups)) ? g.groups : [];
    } catch {
      // ignore
    }

    if (!alive) return;

    renderUpstreamCard();
    renderKeysCard();
    renderGuides();
    renderTester();
  }

  const guidesCard = h('div', { class: 'card mt' },
    h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Agent & Client Configuration'),
        h('p', { class: 'card-sub' }, 'Select your tool to view setup instructions and copy configuration snippets with your NineGuard key')
      )
    ),
    guidesWrap
  );

  root.append(h('div', { class: 'page' },
    header,
    renderFlowCard(),
    upstreamCard,
    h('div', { class: 'mt' }, renderUrlCard()),
    keysCard,
    guidesCard,
    testerCard
  ));

  load();

  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
