// Providers view: Manage multiple OpenAI-compatible upstream providers with custom routing prefixes.
import { api } from '../api.js';
import { h, icon, toast, emptyState, formDialog, confirmDialog, passwordField } from '../ui.js';

export function mount(root) {
  let alive = true;
  let providersList = [];

  const listContainer = h('div', { class: 'mt' });

  // ── Header ──
  const addBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    onclick: () => openProviderModal()
  }, icon('plus'), 'Add Provider');

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Upstream Providers (OpenAI-Compatible)'),
      h('p', null, 'Configure multiple OpenAI-compatible upstream providers. Set custom prefixes (e.g. openrouter, local) to group models as prefix/model-name.')
    ),
    h('div', { class: 'page-actions' }, addBtn)
  );

  // ── Flow Explanation Card ──
  function renderFlowCard() {
    return h('div', { class: 'card' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Multi-Provider Routing & Prefix Grouping'),
          h('p', { class: 'card-sub' }, 'NineGuard automatically prefixes model lists and strips prefixes when forwarding to each provider')
        )
      ),
      h('div', { style: { padding: '14px', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '12px' } },
        h('div', { style: { padding: '12px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '6px', fontWeight: 'bold' } },
            icon('box'), '1. Model Discovery (GET /v1/models)'
          ),
          h('p', { class: 'muted', style: { fontSize: '11.5px', margin: 0, lineHeight: '1.5' } },
            'NineGuard aggregates models from all providers. A provider with prefix ',
            h('code', null, 'openrouter'), ' exposes models as ', h('code', { style: { color: 'var(--accent)' } }, 'openrouter/anthropic/claude-3.5-sonnet'), '.'
          )
        ),
        h('div', { style: { padding: '12px', borderRadius: '8px', border: '1.5px solid var(--accent)', background: 'var(--hover)' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '6px', fontWeight: 'bold', color: 'var(--accent)' } },
            icon('shield'), '2. Intelligent Router'
          ),
          h('p', { style: { fontSize: '11.5px', margin: 0, lineHeight: '1.5' } },
            'When an agent calls ', h('code', null, 'openrouter/...'), ', NineGuard strips ', h('code', null, 'openrouter/'), ', injects that provider\'s API key, and routes to its endpoint.'
          )
        ),
        h('div', { style: { padding: '12px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '6px', fontWeight: 'bold' } },
            icon('server'), '3. Upstream Dispatch'
          ),
          h('p', { class: 'muted', style: { fontSize: '11.5px', margin: 0, lineHeight: '1.5' } },
            'Requests with no prefix fall back to the Default Provider. Tokens and requests are tracked accurately per agent key.'
          )
        )
      )
    );
  }

  // ── Render Providers List ──
  function renderProviders() {
    if (!providersList.length) {
      listContainer.replaceChildren(
        emptyState('server', 'No Upstream Providers Configured', 'Click "Add Provider" to register your first OpenAI-compatible upstream endpoint.')
      );
      return;
    }

    const cards = providersList.map((p) => {
      const probeResult = h('div', { style: { display: 'none', marginTop: '10px', padding: '8px 12px', borderRadius: '6px', fontSize: '11.5px' } });

      const testBtn = h('button', {
        class: 'btn btn-sm',
        type: 'button',
        onclick: async () => {
          testBtn.disabled = true;
          testBtn.textContent = 'Testing...';
          probeResult.style.display = 'block';
          probeResult.style.background = 'var(--hover)';
          probeResult.replaceChildren(h('span', { class: 'muted' }, 'Pinging ', p.route, '...'));

          try {
            const res = await api.post('/providers/test', { route: p.route, api_key: p.api_key });
            if (res.ok) {
              probeResult.style.background = 'color-mix(in srgb, var(--ok) 12%, var(--panel))';
              probeResult.style.border = '1px solid var(--ok)';
              probeResult.replaceChildren(
                h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
                  h('span', { style: { color: 'var(--ok)', fontWeight: 'bold' } }, icon('check'), ' Connected (HTTP 200 OK)'),
                  h('span', { class: 'muted' }, `Latency: ${res.latency_ms}ms · Models: ${res.models_count || 0}`)
                )
              );
            } else {
              probeResult.style.background = 'color-mix(in srgb, var(--danger) 12%, var(--panel))';
              probeResult.style.border = '1px solid var(--danger)';
              probeResult.replaceChildren(
                h('span', { style: { color: 'var(--danger)', fontWeight: 'bold' } }, icon('alert'), ' Failed: '),
                h('span', null, res.error || `HTTP ${res.status}`)
              );
            }
          } catch (err) {
            probeResult.style.background = 'color-mix(in srgb, var(--danger) 12%, var(--panel))';
            probeResult.replaceChildren(h('span', { style: { color: 'var(--danger)' } }, `Error: ${err.message}`));
          } finally {
            testBtn.disabled = false;
            testBtn.replaceChildren(icon('refresh'), 'Test Probe');
          }
        }
      }, icon('refresh'), 'Test Probe');

      const statusBadge = h('button', {
        class: `badge ${p.is_active ? 'ok' : 'muted'} badge-btn`,
        type: 'button',
        onclick: async () => {
          try {
            await api.post(`/providers/${p.id}/toggle`, { active: !p.is_active });
            toast(`Provider ${p.is_active ? 'disabled' : 'enabled'}`);
            load();
          } catch (e) {
            toast(`Failed: ${e.message}`, 'error');
          }
        }
      }, p.is_active ? 'Active' : 'Disabled');

      const prefixTag = p.prefix
        ? h('span', { class: 'badge', style: { background: 'var(--accent-soft)', color: 'var(--accent-text)', fontWeight: 'bold' } },
            `prefix: ${p.prefix}/`
          )
        : h('span', { class: 'badge muted' }, 'no prefix');

      const defaultTag = p.is_default
        ? h('span', { class: 'badge ok', style: { fontSize: '10px' } }, 'Default Fallback')
        : null;

      return h('div', { class: 'card', style: { marginBottom: '12px' } },
        h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
            icon('server'),
            h('h2', { style: { margin: 0, fontSize: '14px' } }, p.name),
            prefixTag,
            defaultTag
          ),
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
            statusBadge,
            h('button', {
              class: 'btn btn-sm',
              type: 'button',
              onclick: () => openProviderModal(p)
            }, icon('pencil'), 'Edit'),
            h('button', {
              class: 'btn btn-sm btn-danger',
              type: 'button',
              onclick: () => confirmDeleteProvider(p)
            }, icon('trash'))
          )
        ),
        h('div', { style: { padding: '12px 16px', background: 'var(--hover)', borderRadius: '6px', margin: '8px 0 0 0', display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'center', gap: '10px' } },
          h('div', null,
            h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase' } }, 'Endpoint Route'),
            h('code', { style: { fontWeight: 'bold' } }, p.route)
          ),
          h('div', null,
            h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase' } }, 'Upstream API Key'),
            h('code', { class: 'muted' }, p.masked_key || '(None required)')
          ),
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
            testBtn
          )
        ),
        probeResult
      );
    });

    listContainer.replaceChildren(...cards);
  }

  // ── Add / Edit Provider Modal ──
  function openProviderModal(existing = null) {
    const isEdit = !!existing;

    const nameInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. OpenRouter, Ollama Server, vLLM',
      value: existing ? existing.name : ''
    });

    const prefixInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. openrouter, local, ollama (leave blank for root)',
      value: existing ? existing.prefix : ''
    });

    const routeInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. https://api.openai.com or http://localhost:11434',
      value: existing ? existing.route : ''
    });

    const keyField = passwordField({
      placeholder: 'Paste upstream API key (leave empty if not needed)',
      value: existing ? (existing.api_key || '') : ''
    });

    const defaultCheck = h('input', {
      type: 'checkbox',
      checked: existing ? existing.is_default : (providersList.length === 0)
    });

    const defaultLabel = h('label', { style: { display: 'inline-flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '12px' } },
      defaultCheck,
      h('span', null, 'Set as Default Provider (fallback for models without prefix)')
    );

    formDialog({
      title: isEdit ? `Edit Provider: ${existing.name}` : 'Add OpenAI-Compatible Provider',
      submitText: isEdit ? 'Save Changes' : 'Add Provider',
      fields: [
        {
          node: h('div', { class: 'field' },
            h('span', null, 'Provider Name'),
            nameInput
          )
        },
        {
          node: h('div', { class: 'field' },
            h('span', null, 'Model Routing Prefix'),
            prefixInput,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '2px 0 0' } },
              'Models from this provider will be grouped with this prefix, e.g. ',
              h('code', null, 'openrouter/anthropic/claude-3.5-sonnet'), '. When an agent calls that model, NineGuard routes here.'
            )
          )
        },
        {
          node: h('div', { class: 'field' },
            h('span', null, 'Route / Target URL'),
            routeInput,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '2px 0 0' } },
              'The HTTP base endpoint where this OpenAI-compatible server is listening (e.g. https://api.openai.com or http://localhost:11434).'
            )
          )
        },
        {
          node: h('div', { class: 'field' },
            h('span', null, 'Upstream API Key'),
            keyField.wrap,
            h('p', { class: 'muted', style: { fontSize: '11px', margin: '2px 0 0' } },
              'The API key required by this upstream provider. NineGuard injects this key when relaying requests.'
            )
          )
        },
        { node: defaultLabel }
      ],
      onSubmit: async () => {
        const name = nameInput.value.trim();
        const route = routeInput.value.trim();
        const prefix = prefixInput.value.trim();
        const apiKey = keyField.input.value.trim();
        const isDef = defaultCheck.checked;

        if (!name) throw new Error('Provider name is required');
        if (!route) throw new Error('Route target URL is required');

        if (isEdit) {
          await api.put(`/providers/${existing.id}`, {
            name, route, prefix, api_key: apiKey, is_default: isDef, is_active: existing.is_active
          });
          toast(`Provider "${name}" updated`, 'ok');
        } else {
          await api.post('/providers', {
            name, route, prefix, api_key: apiKey, is_default: isDef, is_active: true
          });
          toast(`Provider "${name}" added`, 'ok');
        }
        await load();
      }
    });
  }

  async function confirmDeleteProvider(p) {
    const ok = await confirmDialog({
      title: `Delete Provider "${p.name}"?`,
      body: `Requests using prefix "${p.prefix}/" will no longer be routed. Are you sure you want to delete this provider?`,
      confirmText: 'Delete Provider',
      danger: true,
    });
    if (!ok) return;

    try {
      await api.del(`/providers/${p.id}`);
      toast(`Provider "${p.name}" deleted`, 'ok');
      await load();
    } catch (e) {
      toast(`Failed: ${e.message}`, 'error');
    }
  }

  // ── Load Providers ──
  async function load() {
    try {
      const res = await api.get('/providers');
      providersList = (res && res.providers) ? res.providers : [];
    } catch {
      providersList = [];
    }
    if (!alive) return;
    renderProviders();
  }

  root.append(h('div', { class: 'page' },
    header,
    renderFlowCard(),
    listContainer
  ));

  load();

  return {
    update() {},
    refresh: load,
    destroy() { alive = false; }
  };
}
