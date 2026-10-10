// Plugins view: Manage token savers, prompt injectors, HTTP filters, pipeline order, and scope bindings.
import { api } from '../api.js';
import { h, icon, toast, formDialog, confirmDialog, copy } from '../ui.js';
import { store } from '../state.js';
import { scopeLabel, scopeSubLabel, formatInheritText, sortPipeline, warningBannerText } from '../pluginhelpers.js';

function promptSecretGuardMode(currentAction = 'block', onSave) {
  let selected = currentAction || 'block';

  const modes = [
    {
      id: 'block',
      title: '🛡️ Block Request (Recommended)',
      desc: 'Immediately rejects the request with HTTP 403 Forbidden before it reaches upstream LLM. Prevents any accidental leak. Ideal for sensitive company codebases.',
      badge: 'Strict'
    },
    {
      id: 'redact',
      title: '✂️ Redact In-Flight',
      desc: 'Replaces detected secrets with [REDACTED_SECRET:<type>] in real-time, allowing the LLM request to complete without leaking actual credentials.',
      badge: 'Safe Pass'
    },
    {
      id: 'warn_only',
      title: '⚠️ Warn Only (Audit Mode)',
      desc: 'Forwards the prompt unmodified to the model, but records a WARN security audit in System Logs. Useful for testing rule sensitivity without blocking agents.',
      badge: 'Monitor'
    }
  ];

  const radioContainer = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '8px', margin: '8px 0' } });

  function renderOptions() {
    radioContainer.replaceChildren(
      ...modes.map(m => {
        const isSel = selected === m.id;
        return h('label', {
          style: {
            display: 'flex',
            alignItems: 'flex-start',
            gap: '10px',
            padding: '10px 12px',
            borderRadius: '8px',
            border: `1px solid ${isSel ? 'var(--accent, #6366f1)' : 'var(--border)'}`,
            background: isSel ? 'var(--hover)' : 'var(--panel)',
            cursor: 'pointer'
          },
          onclick: () => {
            selected = m.id;
            renderOptions();
          }
        },
          h('input', {
            type: 'radio',
            name: 'sg_mode',
            value: m.id,
            checked: isSel,
            style: { marginTop: '3px' }
          }),
          h('div', { style: { flex: 1 } },
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '3px' } },
              h('b', { style: { fontSize: '13px' } }, m.title),
              h('span', { class: 'badge', style: { fontSize: '10px' } }, m.badge)
            ),
            h('p', { class: 'muted', style: { fontSize: '11.5px', margin: 0, lineHeight: 1.4 } }, m.desc)
          )
        );
      })
    );
  }
  renderOptions();

  formDialog({
    title: 'SecretGuard: Select Guardrail Mode',
    submitText: 'Enable SecretGuard',
    wide: true,
    fields: [
      {
        label: 'Choose how SecretGuard handles detected secrets (API keys, private keys, .env credentials):',
        node: radioContainer
      }
    ],
    onSubmit: async () => {
      await onSave(selected);
    }
  });
}

// Shows a plugin secret once, with a Copy button.
function showSecretDialog(secret, title = 'Plugin shared secret') {
  const field = h('input', { class: 'input', value: secret, readOnly: true, 'aria-label': 'Plugin secret', style: { flex: '1', minWidth: '0', fontFamily: 'var(--font)' } });
  field.addEventListener('focus', () => field.select());
  return formDialog({
    title,
    body: h('div', null,
      h('p', null, 'Copy this secret now. It will not be shown again. Your plugin must check it in the X-NineGuard-Plugin-Secret header.'),
      h('div', { style: { display: 'flex', gap: '8px', marginTop: '8px' } },
        field,
        h('button', { class: 'btn', type: 'button', onclick: () => copy(secret, 'Secret copied') }, icon('copy'), 'Copy'))),
    submitText: 'Done',
    cancel: false,
    onSubmit: async () => true,
  });
}

// Result line for an endpoint test: reachable with latency, or what went wrong.
async function probeEndpoint(url, timeoutMs, out, btn) {
  const target = (url || '').trim();
  if (!/^https?:\/\//i.test(target)) {
    out.className = 'form-error';
    out.textContent = 'Enter a URL that starts with http:// or https:// first.';
    return;
  }
  btn.disabled = true;
  out.className = 'muted';
  out.textContent = 'Testing...';
  try {
    const res = await api.post('/plugins/test-url', { url: target, timeout_ms: parseInt(timeoutMs || '3000', 10) });
    out.className = 'muted';
    out.style.color = 'var(--ok)';
    out.textContent = `Reachable, answered in ${res.latency_ms || 0}ms`;
  } catch (e) {
    out.className = 'form-error';
    out.style.color = '';
    out.textContent = `Not reachable: ${e.message}`;
  } finally {
    btn.disabled = false;
  }
}

export function mount(root) {
  let alive = true;
  let pluginsList = [];
  let groupsList = [];
  let keysList = [];
  let modelsList = [];
  let warningsList = [];

  const container = h('div', { class: 'page' });

  // ── Header ──
  const isAdmin = () => !store.authEnabled || store.role === 'admin';

  const registerBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    style: { display: isAdmin() ? 'inline-flex' : 'none' },
    onclick: () => openRegisterModal()
  }, icon('plus'), 'Register HTTP Plugin');

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Plugins & Token Savers'),
      h('p', null, 'Reduce token usage (Headroom, Ponytail, Caveman) or run custom HTTP filters. Key beats Group beats All keys, all models.')
    ),
    h('div', { class: 'page-actions' }, registerBtn)
  );

  // ── First-view Upgrade Banner ──
  function renderUpgradeBanner() {
    const ack = localStorage.getItem('ng.plugins.ack.banner');
    if (ack) return null;

    const banner = h('div', {
      class: 'card',
      style: {
        background: 'var(--hover)',
        borderColor: 'var(--accent)',
        marginBottom: '16px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '12px 16px'
      }
    },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px' } },
        icon('info', 'text-accent'),
        h('span', null,
          h('b', null, 'Plugins available'),
          ' — all token savers are off by default. Precedence: ',
          h('code', null, 'Key beats Group beats All keys, all models'), '.'
        )
      ),
      h('button', {
        class: 'btn btn-sm',
        onclick: () => {
          localStorage.setItem('ng.plugins.ack.banner', '1');
          banner.remove();
        }
      }, 'Dismiss')
    );
    return banner;
  }

  // ── Warnings Banner ──
  function renderWarningsBanner() {
    if (!warningsList.length) return null;
    return h('div', {
      class: 'card',
      style: {
        background: 'rgba(239, 68, 68, 0.08)',
        borderColor: 'var(--danger)',
        marginBottom: '16px',
        padding: '14px'
      }
    },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 'bold', color: 'var(--danger)', marginBottom: '8px' } },
        icon('alert'), warningBannerText(warningsList.length)
      ),
      h('ul', { style: { margin: 0, paddingLeft: '20px', fontSize: '12px' } },
        ...warningsList.slice(0, 5).map((w) =>
          h('li', { style: { marginBottom: '4px' } },
            h('b', null, w.key_name ? `${w.key_name}: ` : ''),
            w.message
          )
        )
      )
    );
  }

  // ── Plugin Cards List ──
  const listEl = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px', marginBottom: '24px' } });

  function renderList() {
    const sorted = sortPipeline(pluginsList);
    if (!sorted.length) {
      listEl.replaceChildren(h('div', { class: 'card', style: { padding: '24px', textAlign: 'center' } }, 'No plugins registered'));
      return;
    }

    listEl.replaceChildren(...sorted.map((p, idx) => {
      const globBinding = p.global_binding || { state: 'off' };
      const isGlobOn = globBinding.state === 'on';

      const catBadge = h('span', { class: 'badge', style: { textTransform: 'capitalize' } },
        p.category === 'input_compression' ? 'Input Compression' :
        p.category === 'output_style' ? 'Output Style' : 'Other'
      );

      const policyBadge = h('span', {
        class: `badge ${p.failure_policy === 'closed' ? 'badge-danger' : 'badge-neutral'}`
      }, p.failure_policy === 'closed' ? 'Fail Closed' : 'Fail Open');

      const bypassBadge = h('span', { class: 'badge' }, p.bypassable ? 'Bypassable' : 'Strict');

      const latencyLabel = h('span', { class: 'muted', style: { fontSize: '11px', marginLeft: '6px' } });
      const testBtn = h('button', {
        class: 'btn btn-sm',
        onclick: async () => {
          testBtn.disabled = true;
          latencyLabel.textContent = 'Testing...';
          try {
            const res = await api.post(`/plugins/${encodeURIComponent(p.id)}/test`);
            if (res.in_process) {
              latencyLabel.textContent = 'In-process (active)';
            } else if (res.status === 'error') {
              latencyLabel.textContent = `Error: ${res.error || 'Failed'} (${res.latency_ms || 0}ms)`;
            } else {
              latencyLabel.textContent = `${res.latency_ms || 0}ms (${res.status || 'ok'})`;
            }
          } catch (e) {
            latencyLabel.textContent = `Error: ${e.message}`;
          } finally {
            testBtn.disabled = false;
          }
        }
      }, icon('play'), 'Test');

      const toggleGlobBtn = h('button', {
        class: `btn btn-sm ${isGlobOn ? 'btn-primary' : ''}`,
        onclick: async () => {
          const nextState = isGlobOn ? 'off' : 'on';
          if (nextState === 'on' && p.id === 'secretguard') {
            let act = 'block';
            try {
              if (p.default_settings) act = JSON.parse(p.default_settings).action || 'block';
            } catch {}
            promptSecretGuardMode(act, async (mode) => {
              await api.put(`/plugins/${encodeURIComponent(p.id)}`, {
                default_settings: JSON.stringify({ action: mode })
              });
              await saveBinding(p.id, 'global', '', 'on', JSON.stringify({ action: mode }));
              reload();
            });
            return;
          }
          if (nextState === 'on' && (p.category === 'input_compression' || p.category === 'output_style')) {
            const ackKey = `ng.plugins.ack.${p.id}`;
            if (!localStorage.getItem(ackKey)) {
              confirmDialog({
                title: `Enable ${p.name} globally?`,
                message: `Check that your upstream providers do not already apply token saving (e.g. 9router RTK, another NineGuard). Stacked token savers can remove context and lower answer quality.\n\nTurn on for All keys, all models?`,
                confirmText: 'Enable',
                onConfirm: async () => {
                  await saveBinding(p.id, 'global', '', nextState, '{}');
                  reload();
                }
              });
              return;
            }
          }
          await saveBinding(p.id, 'global', '', nextState, '{}');
          reload();
        }
      }, isGlobOn ? 'All keys, all models: ON' : 'All keys, all models: OFF');

      const configBtn = h('button', {
        class: 'btn btn-sm',
        onclick: () => openDrawer(p)
      }, icon('pencil'), 'Configure & Scopes');

      const moveUpBtn = h('button', {
        class: 'btn btn-sm',
        disabled: idx === 0,
        title: 'Move up in pipeline',
        onclick: () => reorder(idx, idx - 1)
      }, icon('chevron-left'));

      const moveDownBtn = h('button', {
        class: 'btn btn-sm',
        disabled: idx === sorted.length - 1,
        title: 'Move down in pipeline',
        onclick: () => reorder(idx, idx + 1)
      }, icon('chevron-right'));

      const guidanceCard = p.guidance ? h('div', {
        class: 'muted',
        style: {
          fontSize: '11.5px',
          marginTop: '8px',
          background: 'var(--panel)',
          padding: '8px 12px',
          borderRadius: '6px'
        }
      },
        h('div', null, h('b', null, 'Summary: '), p.guidance.summary || ''),
        p.guidance.recommended_for ? h('div', { style: { marginTop: '2px' } }, h('b', null, 'Recommended for: '), p.guidance.recommended_for) : null,
        p.guidance.not_recommended_for ? h('div', { style: { marginTop: '2px', color: 'var(--warning, #eab308)' } }, h('b', null, 'Not recommended: '), p.guidance.not_recommended_for) : null
      ) : null;

      return h('div', { class: 'card', style: { padding: '14px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '8px' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
            h('span', { class: 'badge', style: { fontWeight: 'bold' } }, `#${p.pipeline_order}`),
            h('h3', { style: { margin: 0, fontSize: '15px' } }, p.name),
            h('span', { class: 'badge' }, p.kind),
            catBadge,
            policyBadge,
            bypassBadge
          ),
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' } },
            moveUpBtn, moveDownBtn,
            toggleGlobBtn,
            testBtn, latencyLabel,
            configBtn
          )
        ),
        p.description ? h('p', { class: 'muted', style: { fontSize: '12px', margin: '6px 0 0' } }, p.description) : null,
        guidanceCard
      );
    }));
  }

  // ── Reorder pipeline ──
  async function reorder(fromIdx, toIdx) {
    const sorted = sortPipeline(pluginsList);
    const item = sorted.splice(fromIdx, 1)[0];
    sorted.splice(toIdx, 0, item);
    const orderedIDs = sorted.map((p) => p.id);
    try {
      await api.put('/plugins/order', { order: orderedIDs });
      toast('Pipeline order updated');
      reload();
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  // ── Save Binding ──
  async function saveBinding(pluginID, scopeType, scopeID, state, settings = '{}') {
    try {
      const res = await api.put(`/plugins/${encodeURIComponent(pluginID)}/bindings`, {
        scope_type: scopeType, scope_id: scopeID, state, settings
      });
      toast(`Binding updated: ${state}`);
      if (res.warnings && res.warnings.length) {
        toast(`Notice: ${res.warnings[0].message}`, 'warning');
      }
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  // ── Configure Drawer ──
  async function openDrawer(p) {
    let bindings = [];
    try {
      const bRes = await api.get(`/plugins/${encodeURIComponent(p.id)}/bindings`);
      bindings = bRes.bindings || [];
    } catch { /* ignore */ }

    const bindingMap = new Map();
    for (const b of bindings) {
      bindingMap.set(`${b.scope_type}:${b.scope_id}`, b);
    }

    // 1. Global Rule Panel
    const globKey = 'global:';
    const globB = bindingMap.get(globKey) || { state: 'off', settings: '{}' };
    const globSelect = h('select', { class: 'input', style: { width: '130px', fontWeight: 'bold' } },
      h('option', { value: 'off', selected: globB.state === 'off' }, 'Off (Disabled)'),
      h('option', { value: 'on', selected: globB.state === 'on' }, 'On (Enabled)')
    );
    globSelect.onchange = async () => {
      if (globSelect.value === 'on' && p.id === 'secretguard') {
        let act = 'block';
        try {
          if (globB.settings) act = JSON.parse(globB.settings).action || 'block';
        } catch {}
        promptSecretGuardMode(act, async (mode) => {
          globB.settings = JSON.stringify({ action: mode });
          await saveBinding(p.id, 'global', '', 'on', globB.settings);
          reload();
        });
        return;
      }
      await saveBinding(p.id, 'global', '', globSelect.value, globB.settings);
      reload();
    };

    const globalPanel = h('div', {
      style: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '12px 14px',
        borderRadius: '6px',
        border: '1px solid var(--border)',
        background: 'var(--panel)',
        marginBottom: '16px'
      }
    },
      h('div', null,
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
          h('b', { style: { fontSize: '13px' } }, scopeLabel('global')),
          h('span', { class: 'badge', style: { fontSize: '10px' } }, 'Base Rule')
        ),
        h('div', { class: 'muted', style: { fontSize: '11px', marginTop: '2px' } }, scopeSubLabel('global'))
      ),
      globSelect
    );

    // 2. Active Scope Overrides Table
    const overridesContainer = h('div');

    function renderOverrides() {
      const activeList = [];
      for (const [key, b] of bindingMap.entries()) {
        if (b.scope_type === 'global') continue;
        if (b.state === 'on' || b.state === 'off') {
          activeList.push(b);
        }
      }

      if (!activeList.length) {
        overridesContainer.replaceChildren(
          h('div', { class: 'muted', style: { padding: '16px', textAlign: 'center', border: '1px dashed var(--border)', borderRadius: '6px', fontSize: '12px' } },
            'No scope overrides configured. All model groups and API keys currently inherit the base rule above.'
          )
        );
        return;
      }

      const rows = activeList.map((b) => {
        let name = b.scope_id;
        let subText = '';
        if (b.scope_type === 'group') {
          const grp = groupsList.find((g) => g.id === b.scope_id);
          name = grp ? grp.name : b.scope_id;
          subText = grp ? `${grp.models_count || 0} models · any key` : '';
        } else if (b.scope_type === 'key') {
          const k = keysList.find((key) => key.id === b.scope_id);
          name = k ? k.name : b.scope_id;
          subText = 'Any model this key uses';
        }

        const stateSelect = h('select', { class: 'input', style: { width: '90px' } },
          h('option', { value: 'on', selected: b.state === 'on' }, 'On'),
          h('option', { value: 'off', selected: b.state === 'off' }, 'Off')
        );
        stateSelect.onchange = async () => {
          b.state = stateSelect.value;
          await saveBinding(p.id, b.scope_type, b.scope_id, stateSelect.value, b.settings || '{}');
          reload();
        };

        const removeBtn = h('button', {
          class: 'btn btn-sm btn-danger',
          type: 'button',
          title: 'Reset to Inherit',
          onclick: async () => {
            bindingMap.delete(`${b.scope_type}:${b.scope_id}`);
            await saveBinding(p.id, b.scope_type, b.scope_id, 'inherit', '{}');
            renderOverrides();
            updatePickerOptions();
            reload();
          }
        }, icon('trash'), 'Remove');

        return h('tr', null,
          h('td', null,
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
              h('span', { class: 'badge', style: { textTransform: 'capitalize', fontSize: '10px' } }, b.scope_type),
              h('b', null, scopeLabel(b.scope_type, name))
            ),
            subText ? h('div', { class: 'muted', style: { fontSize: '10.5px', marginTop: '2px' } }, subText) : null
          ),
          h('td', null, stateSelect),
          h('td', { style: { textAlign: 'right' } }, removeBtn)
        );
      });

      overridesContainer.replaceChildren(
        h('table', { class: 'table', style: { width: '100%', fontSize: '12px' } },
          h('thead', null,
            h('tr', null,
              h('th', null, 'Target Scope'),
              h('th', { style: { width: '110px' } }, 'Override Rule'),
              h('th', { style: { width: '90px', textAlign: 'right' } }, 'Action')
            )
          ),
          h('tbody', null, ...rows)
        )
      );
    }

    // 3. Add Override Picker
    const scopePicker = h('select', { class: 'input', style: { flex: '1', minWidth: '220px' } });
    const rulePicker = h('select', { class: 'input', style: { width: '90px' } },
      h('option', { value: 'on' }, 'On'),
      h('option', { value: 'off' }, 'Off')
    );

    function updatePickerOptions() {
      const opts = [h('option', { value: '' }, '-- Select Group or Key to Override --')];

      const groupOpts = groupsList
        .filter((g) => {
          const existing = bindingMap.get(`group:${g.id}`);
          return !existing || (existing.state !== 'on' && existing.state !== 'off');
        })
        .map((g) => h('option', { value: `group:${g.id}` }, `Group: ${g.name} (${g.models_count || 0} models)`));

      if (groupOpts.length > 0) {
        opts.push(h('optgroup', { label: 'Model Groups' }, ...groupOpts));
      }

      const keyOpts = keysList
        .filter((k) => {
          const existing = bindingMap.get(`key:${k.id}`);
          return !existing || (existing.state !== 'on' && existing.state !== 'off');
        })
        .map((k) => h('option', { value: `key:${k.id}` }, `Key: ${k.name} (${k.key})`));

      if (keyOpts.length > 0) {
        opts.push(h('optgroup', { label: 'API Keys' }, ...keyOpts));
      }

      scopePicker.replaceChildren(...opts);
    }

    updatePickerOptions();
    renderOverrides();

    const addOverrideBtn = h('button', {
      class: 'btn btn-primary btn-sm',
      type: 'button',
      onclick: async () => {
        const val = scopePicker.value;
        if (!val) return toast('Please select a Model Group or API Key', 'warn');
        const [scopeType, scopeID] = val.split(':');
        const state = rulePicker.value;

        addOverrideBtn.disabled = true;
        try {
          bindingMap.set(`${scopeType}:${scopeID}`, {
            plugin_id: p.id,
            scope_type: scopeType,
            scope_id: scopeID,
            state: state,
            settings: '{}'
          });
          await saveBinding(p.id, scopeType, scopeID, state, '{}');
          renderOverrides();
          updatePickerOptions();
          reload();
        } finally {
          addOverrideBtn.disabled = false;
        }
      }
    }, icon('plus'), 'Add Override');

    const addOverrideRow = h('div', {
      style: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        flexWrap: 'wrap',
        padding: '10px 12px',
        background: 'var(--hover)',
        borderRadius: '6px',
        marginTop: '12px'
      }
    },
      scopePicker,
      rulePicker,
      addOverrideBtn
    );

    // Settings Section (typed fields for built-ins, prompt override editor)
    let settingsSection = null;
    let currentSettings = {};
    try {
      currentSettings = JSON.parse(p.default_settings || '{}');
    } catch { currentSettings = {}; }

    if (p.id === 'headroom') {
      const urlInput = h('input', {
        class: 'input',
        type: 'text',
        value: currentSettings.url || 'http://127.0.0.1:8787',
        placeholder: 'e.g. http://127.0.0.1:8787'
      });
      const modeSelect = h('select', { class: 'input' },
        h('option', { value: 'incremental', selected: (currentSettings.mode || 'incremental') === 'incremental' }, 'Incremental (compress newest turns only, preserves prefix cache)'),
        h('option', { value: 'full', selected: currentSettings.mode === 'full' }, 'Full (compress entire message history every turn)')
      );
      const compressUserCheck = h('input', {
        type: 'checkbox',
        checked: Boolean(currentSettings.compress_user_messages)
      });
      const tokenInput = h('input', {
        class: 'input',
        type: 'password',
        value: currentSettings.token || '',
        placeholder: 'Optional HEADROOM_PROXY_TOKEN'
      });

      const saveSettingsBtn = h('button', {
        class: 'btn btn-sm btn-primary',
        type: 'button',
        onclick: async () => {
          saveSettingsBtn.disabled = true;
          try {
            const newSettings = {
              url: urlInput.value.trim() || 'http://127.0.0.1:8787',
              mode: modeSelect.value,
              compress_user_messages: Boolean(compressUserCheck.checked),
              token: tokenInput.value.trim()
            };
            await api.put(`/plugins/${encodeURIComponent(p.id)}`, {
              default_settings: JSON.stringify(newSettings)
            });
            toast('Headroom settings saved');
            reload();
          } catch (e) {
            toast(e.message, 'error');
          } finally {
            saveSettingsBtn.disabled = false;
          }
        }
      }, 'Save Headroom Settings');

      settingsSection = h('div', { style: { marginBottom: '16px', padding: '14px', background: 'var(--hover)', borderRadius: '6px' } },
        h('h4', { style: { margin: '0 0 10px', fontSize: '13px' } }, 'Headroom Connector Settings'),
        h('div', { class: 'field', style: { marginBottom: '8px' } },
          h('span', null, 'Headroom Endpoint URL'),
          urlInput,
          h('p', { class: 'muted', style: { fontSize: '11px', margin: '2px 0 0' } }, 'Default port is 8787. When Headroom runs in Docker, ensure HEADROOM_COMPRESS_ALLOW_REMOTE=1 is set.')
        ),
        h('div', { class: 'field', style: { marginBottom: '8px' } },
          h('span', null, 'Compression Mode'),
          modeSelect
        ),
        h('div', { class: 'field', style: { marginBottom: '8px' } },
          h('label', { style: { display: 'inline-flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '12px' } },
            compressUserCheck,
            h('span', null, 'Compress user messages in addition to assistant/tool messages')
          )
        ),
        h('div', { class: 'field', style: { marginBottom: '10px' } },
          h('span', null, 'Proxy Token (Optional)'),
          tokenInput
        ),
        saveSettingsBtn
      );
    } else if (p.id === 'caveman' || p.id === 'ponytail') {
      const isCave = p.id === 'caveman';
      const variantSelect = isCave ? h('select', { class: 'input' },
        h('option', { value: 'caveman', selected: (currentSettings.variant || 'caveman') === 'caveman' }, 'caveman (terse, drops filler, ~4.0 KB prompt)'),
        h('option', { value: 'ultracave', selected: currentSettings.variant === 'ultracave' }, 'ultracave (maximum brevity, ~2.3 KB prompt)'),
        h('option', { value: 'megacave', selected: currentSettings.variant === 'megacave' }, 'megacave (classical Chinese 文言文, ~2.6 KB prompt)')
      ) : h('select', { class: 'input' },
        h('option', { value: 'full', selected: (currentSettings.level || 'full') === 'full' }, 'full (balanced concise formatting)'),
        h('option', { value: 'lite', selected: currentSettings.level === 'lite' }, 'lite (mild compression, keeps explanations)'),
        h('option', { value: 'ultra', selected: currentSettings.level === 'ultra' }, 'ultra (maximum density, no pleasantries)')
      );

      const promptOverrideInput = h('textarea', {
        class: 'input',
        rows: 3,
        placeholder: 'Leave blank to use bundled default prompt text',
        value: currentSettings.prompt_override || '',
        style: {
          fontFamily: 'monospace',
          fontSize: '11.5px',
          padding: '8px 10px',
          lineHeight: '1.5',
          height: 'auto',
          minHeight: '72px',
          boxSizing: 'border-box',
          resize: 'vertical'
        }
      });

      const savePromptBtn = h('button', {
        class: 'btn btn-sm btn-primary',
        type: 'button',
        onclick: async () => {
          savePromptBtn.disabled = true;
          try {
            const newSettings = isCave ? {
              variant: variantSelect.value,
              prompt_override: promptOverrideInput.value.trim()
            } : {
              level: variantSelect.value,
              prompt_override: promptOverrideInput.value.trim()
            };
            await api.put(`/plugins/${encodeURIComponent(p.id)}`, {
              default_settings: JSON.stringify(newSettings)
            });
            toast('Prompt settings saved');
            reload();
          } catch (e) {
            toast(e.message, 'error');
          } finally {
            savePromptBtn.disabled = false;
          }
        }
      }, 'Save Settings');

      const resetBtn = h('button', {
        class: 'btn btn-sm',
        type: 'button',
        onclick: async () => {
          try {
            await api.post(`/plugins/${encodeURIComponent(p.id)}/reset-prompt`);
            promptOverrideInput.value = '';
            toast('Prompt override reset to default');
            reload();
          } catch (e) {
            toast(e.message, 'error');
          }
        }
      }, 'Reset Prompt to Default');

      settingsSection = h('div', { style: { marginBottom: '16px', padding: '14px', background: 'var(--hover)', borderRadius: '6px' } },
        h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
          h('h4', { style: { margin: 0, fontSize: '13px' } }, isCave ? 'Caveman Settings & Prompt' : 'Ponytail Settings & Prompt'),
          resetBtn
        ),
        h('div', { class: 'field', style: { marginBottom: '8px' } },
          h('span', null, isCave ? 'Prompt Variant' : 'Compression Level'),
          variantSelect
        ),
        h('div', { class: 'field', style: { marginBottom: '10px' } },
          h('span', null, 'Custom Prompt Override (Optional)'),
          promptOverrideInput
        ),
        savePromptBtn
      );
    } else if (p.id === 'secretguard') {
      let currentAction = currentSettings.action || 'block';

      const modes = [
        {
          id: 'block',
          title: '🛡️ Block Request (Recommended)',
          desc: 'Immediately rejects the request with HTTP 403 Forbidden before it reaches upstream LLM. Prevents any accidental leak. Ideal for sensitive company codebases.',
          badge: 'Strict'
        },
        {
          id: 'redact',
          title: '✂️ Redact In-Flight',
          desc: 'Replaces detected secrets with [REDACTED_SECRET:<type>] in real-time, allowing the LLM request to complete without leaking actual credentials.',
          badge: 'Safe Pass'
        },
        {
          id: 'warn_only',
          title: '⚠️ Warn Only (Audit Mode)',
          desc: 'Forwards the prompt unmodified to the model, but records a WARN security audit in System Logs. Useful for testing rule sensitivity without blocking agents.',
          badge: 'Monitor'
        }
      ];

      const cardsContainer = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '8px', margin: '8px 0 12px' } });

      function renderCards() {
        cardsContainer.replaceChildren(
          ...modes.map((m) => {
            const isSel = currentAction === m.id;
            return h('label', {
              style: {
                display: 'flex',
                alignItems: 'flex-start',
                gap: '10px',
                padding: '10px 12px',
                borderRadius: '8px',
                border: `1px solid ${isSel ? 'var(--accent, #6366f1)' : 'var(--border)'}`,
                background: isSel ? 'var(--hover)' : 'var(--panel)',
                cursor: 'pointer'
              },
              onclick: () => {
                currentAction = m.id;
                renderCards();
              }
            },
              h('input', {
                type: 'radio',
                name: 'sg_drawer_mode',
                value: m.id,
                checked: isSel,
                style: { marginTop: '3px' }
              }),
              h('div', { style: { flex: 1 } },
                h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '3px' } },
                  h('b', { style: { fontSize: '13px' } }, m.title),
                  h('span', { class: 'badge', style: { fontSize: '10px' } }, m.badge)
                ),
                h('p', { class: 'muted', style: { fontSize: '11.5px', margin: 0, lineHeight: 1.4 } }, m.desc)
              )
            );
          })
        );
      }
      renderCards();

      const saveSecretBtn = h('button', {
        class: 'btn btn-sm btn-primary',
        type: 'button',
        onclick: async () => {
          saveSecretBtn.disabled = true;
          try {
            await api.put(`/plugins/${encodeURIComponent(p.id)}`, {
              default_settings: JSON.stringify({ action: currentAction })
            });
            toast('SecretGuard settings saved', 'ok');
            reload();
          } catch (e) {
            toast(e.message, 'error');
          } finally {
            saveSecretBtn.disabled = false;
          }
        }
      }, 'Save SecretGuard Settings');

      settingsSection = h('div', { style: { marginBottom: '16px', padding: '14px', background: 'var(--hover)', borderRadius: '6px' } },
        h('h4', { style: { margin: '0 0 6px', fontSize: '13px' } }, 'SecretGuard Security Settings'),
        h('p', { class: 'muted', style: { fontSize: '11px', margin: '0 0 8px' } },
          'Scans incoming prompts for private keys (RSA/EC), high-entropy API tokens (sk-, ghp-, AKIA), and sensitive environment variables.'
        ),
        cardsContainer,
        saveSecretBtn
      );
    }

    // HTTP plugins: edit connection details, rotate the secret, or delete (admin only).
    let httpSection = null;
    if (p.kind === 'http' && isAdmin()) {
      const nameIn = h('input', { class: 'input', value: p.name || '', 'aria-label': 'Name' });
      const urlIn = h('input', { class: 'input', value: p.url || '', spellcheck: 'false', 'aria-label': 'Endpoint URL' });
      const timeoutIn = h('input', { class: 'input', type: 'number', min: '100', value: String(p.timeout_ms || 3000), 'aria-label': 'Timeout in milliseconds' });
      const policyIn = h('select', { class: 'select wide', 'aria-label': 'Failure policy' },
        h('option', { value: 'open', selected: p.failure_policy !== 'closed' }, 'Fail Open (skip on error)'),
        h('option', { value: 'closed', selected: p.failure_policy === 'closed' }, 'Fail Closed (block request on error)'));
      const summaryIn = h('input', { class: 'input', value: p.summary || '', placeholder: 'What this plugin does (optional)', 'aria-label': 'Summary' });
      const out = h('span', { class: 'muted', style: { fontSize: '11.5px' }, 'aria-live': 'polite' });
      const testBtn2 = h('button', { class: 'btn btn-sm', type: 'button' }, icon('play'), 'Test endpoint');
      testBtn2.addEventListener('click', () => probeEndpoint(urlIn.value, timeoutIn.value, out, testBtn2));
      const saveBtn = h('button', { class: 'btn btn-sm btn-primary', type: 'button' }, 'Save changes');
      saveBtn.addEventListener('click', async () => {
        saveBtn.disabled = true;
        try {
          await api.put(`/plugins/${encodeURIComponent(p.id)}`, {
            name: nameIn.value.trim(),
            url: urlIn.value.trim(),
            timeout_ms: parseInt(timeoutIn.value || '3000', 10),
            failure_policy: policyIn.value,
            category: p.category,
            bypassable: policyIn.value !== 'closed' && p.bypassable !== false,
            summary: summaryIn.value.trim(),
          });
          toast('Plugin saved', 'ok');
          reload();
        } catch (e) {
          toast(e.message, 'error');
        } finally {
          saveBtn.disabled = false;
        }
      });
      const rotateBtn = h('button', { class: 'btn btn-sm', type: 'button' }, 'Rotate secret');
      rotateBtn.addEventListener('click', () => {
        confirmDialog({
          title: `Rotate secret for ${p.name}?`,
          message: 'The old secret stops working at once. Update your plugin with the new one, or requests to it will fail.',
          confirmText: 'Rotate secret',
          danger: true,
          onConfirm: async () => {
            try {
              const res = await api.post(`/plugins/${encodeURIComponent(p.id)}/rotate-secret`);
              showSecretDialog(res.secret, 'New plugin secret');
            } catch (e) {
              toast(e.message, 'error');
              throw e;
            }
          },
        });
      });
      const deleteBtn = h('button', { class: 'btn btn-sm btn-danger', type: 'button' }, icon('trash'), 'Delete plugin');
      deleteBtn.addEventListener('click', () => {
        confirmDialog({
          title: `Delete ${p.name}?`,
          message: 'This removes the plugin and all its scope rules. Requests stop going through it. This cannot be undone.',
          confirmText: 'Delete plugin',
          danger: true,
          typeToConfirm: p.name,
          onConfirm: async () => {
            try {
              await api.del(`/plugins/${encodeURIComponent(p.id)}`);
              toast('Plugin deleted', 'ok');
              document.querySelector('.dialog')?.closest('.overlay')?.remove();
              reload();
            } catch (e) {
              toast(e.message, 'error');
              throw e;
            }
          },
        });
      });
      const row = (label, el) => h('label', { class: 'field', style: { marginBottom: '8px' } }, h('span', null, label), el);
      httpSection = h('div', { style: { marginBottom: '16px', padding: '14px', background: 'var(--hover)', borderRadius: '6px' } },
        h('h4', { style: { margin: '0 0 8px', fontSize: '13px' } }, 'Connection'),
        row('Name', nameIn),
        row('Endpoint URL', urlIn),
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap', margin: '0 0 8px' } }, testBtn2, out),
        row('Timeout (ms)', timeoutIn),
        row('Failure policy', policyIn),
        row('Summary', summaryIn),
        h('div', { style: { display: 'flex', gap: '8px', flexWrap: 'wrap', marginTop: '4px' } }, saveBtn, rotateBtn, deleteBtn));
    }

    formDialog({
      title: `Configure ${p.name}`,
      wide: true,
      body: h('div', null,
        httpSection,
        settingsSection,
        h('h4', { style: { margin: '0 0 4px', fontSize: '13px' } }, 'Base Rule (Default for All Requests)'),
        globalPanel,
        h('h4', { style: { margin: '16px 0 4px', fontSize: '13px' } }, 'Scope Overrides (Exceptions)'),
        h('p', { class: 'muted', style: { fontSize: '11px', margin: '0 0 10px' } },
          'Explicit overrides for specific Model Groups or API Keys. Precedence: Key beats Group beats All keys, all models.'
        ),
        overridesContainer,
        addOverrideRow
      ),
      submitText: 'Done',
      cancel: false,
      onSubmit: async () => true
    });
  }

  // ── Register HTTP Plugin Modal ──
  function openRegisterModal() {
    const urlInput = h('input', { class: 'input', name: 'url', type: 'text', required: true, autocomplete: 'off', spellcheck: 'false', placeholder: 'http://127.0.0.1:9090/transform' });
    const testOut = h('span', { class: 'muted', style: { fontSize: '11.5px' }, 'aria-live': 'polite' });
    const testEndpointBtn = h('button', { class: 'btn btn-sm', type: 'button' }, icon('play'), 'Test endpoint');
    testEndpointBtn.addEventListener('click', () => {
      const t = document.querySelector('.dialog [name=timeout_ms]');
      probeEndpoint(urlInput.value, t ? t.value : '3000', testOut, testEndpointBtn);
    });

    formDialog({
      title: 'Register HTTP Plugin',
      fields: [
        { label: 'Name', name: 'name', placeholder: 'e.g. PII Filter', required: true, hint: 'Shown in the plugin list and in traffic logs.' },
        { label: 'Endpoint URL', name: 'url', input: urlInput, hint: 'NineGuard sends each request here as JSON. Must start with http:// or https://.' },
        { node: h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' } }, testEndpointBtn, testOut) },
        {
          label: 'Category', name: 'category', type: 'select',
          options: [
            { value: 'other', label: 'Other (Policy, PII, Guardrails)' },
            { value: 'input_compression', label: 'Input Compression' },
            { value: 'output_style', label: 'Output Style' },
          ]
        },
        { label: 'Timeout (ms)', name: 'timeout_ms', type: 'number', value: '3000', hint: 'How long to wait for the plugin before the failure policy applies.' },
        {
          label: 'Failure Policy', name: 'failure_policy', type: 'select',
          options: [
            { value: 'open', label: 'Fail Open (skip on error)' },
            { value: 'closed', label: 'Fail Closed (block request on error)' },
          ]
        },
        { label: 'Summary', name: 'summary', required: false, placeholder: 'What this plugin does (optional)' },
      ],
      onSubmit: async (data) => {
        try {
          const res = await api.post('/plugins', {
            name: data.name,
            url: data.url,
            category: data.category || 'other',
            timeout_ms: parseInt(data.timeout_ms || '3000', 10),
            failure_policy: data.failure_policy || 'open',
            bypassable: true,
            summary: data.summary || '',
          });
          toast('Plugin registered');
          reload();

          if (res.secret) showSecretDialog(res.secret);
          return true;
        } catch (e) {
          toast(e.message, 'error');
          return false;
        }
      }
    });
  }

  // ── Live Plugin & Endpoint Probe ──
  const probeCard = h('div', { class: 'card', style: { padding: '16px' } });

  function renderProbe() {
    const activeKeys = keysList.filter((k) => k.is_active !== false);
    const keySelect = h('select', { class: 'input', style: { minWidth: '220px' } },
      ...activeKeys.map((k) => h('option', { value: k.raw_key || k.key, 'data-id': k.id }, `${k.name} (${k.key})`))
    );

    const activeModels = modelsList.filter((m) => m.enabled !== false);
    const datalistId = 'plugin-probe-models';
    const datalist = h('datalist', { id: datalistId },
      ...activeModels.map((m) => h('option', { value: m.id }))
    );

    const defaultModel = activeModels[0] ? activeModels[0].id : '9router/anthropic/claude-3.5-sonnet';
    const modelInput = h('input', {
      class: 'input',
      list: datalistId,
      placeholder: 'Select or type model...',
      value: defaultModel,
      style: { minWidth: '260px' }
    });

    const promptInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'Test prompt...',
      value: 'Hi NineGuard! Give me a 1-sentence response.',
      style: { flex: '1', minWidth: '260px' }
    });

    const resultBox = h('div', {
      style: { display: 'none', marginTop: '14px', padding: '12px', background: 'var(--hover)', borderRadius: '6px' }
    });

    // 1. Live Ping Button
    const pingBtn = h('button', {
      class: 'btn btn-primary',
      type: 'button',
      onclick: async () => {
        const model = modelInput.value.trim();
        const key = keySelect.value;
        const prompt = promptInput.value.trim();

        if (!model) return toast('No model specified', 'warn');
        if (!key) return toast('No API key selected', 'warn');

        pingBtn.disabled = true;
        pingBtn.textContent = 'Sending...';
        resultBox.style.display = 'block';
        resultBox.replaceChildren(h('span', { class: 'muted' }, 'Relaying through NineGuard proxy /v1/chat/completions...'));

        const startTime = performance.now();
        try {
          const resp = await fetch('/v1/chat/completions', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${key}`,
            },
            body: JSON.stringify({
              model,
              messages: [{ role: 'user', content: prompt || 'ping' }],
              stream: false,
            })
          });

          const duration = Math.round(performance.now() - startTime);
          const pluginsApplied = resp.headers.get('X-NineGuard-Plugins-Applied') || '';
          const tokensSaved = resp.headers.get('X-NineGuard-Tokens-Saved') || '';
          const data = await resp.json().catch(() => null);

          if (resp.ok) {
            const content = data?.choices?.[0]?.message?.content || '(Empty content)';
            const totalTok = data?.usage?.total_tokens || 0;

            const pluginBadges = [];
            if (pluginsApplied) {
              pluginBadges.push(h('span', {
                class: 'badge',
                style: { background: 'rgba(249, 115, 22, 0.15)', color: 'var(--accent)', border: '1px solid rgba(249, 115, 22, 0.4)' }
              }, '🧩 ' + pluginsApplied));
            }
            if (tokensSaved && parseInt(tokensSaved, 10) > 0) {
              pluginBadges.push(h('span', { class: 'badge ok' }, `-${tokensSaved} tokens saved`));
            }

            resultBox.replaceChildren(
              h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px', marginBottom: '8px' } },
                h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
                  h('span', { class: 'badge ok' }, `HTTP ${resp.status} OK`),
                  ...pluginBadges
                ),
                h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms · Tokens: ${totalTok}`)
              ),
              h('div', { style: { fontSize: '13px', whiteSpace: 'pre-wrap', lineHeight: '1.5', fontFamily: 'monospace', padding: '8px', background: 'var(--panel)', borderRadius: '4px' } }, content)
            );
            toast('Live request succeeded!', 'ok');
          } else {
            const errText = data?.error?.message || `HTTP ${resp.status} ${resp.statusText}`;
            resultBox.replaceChildren(
              h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
                h('span', { class: 'badge err' }, `HTTP ${resp.status} Failed`),
                h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms`)
              ),
              h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, errText)
            );
            toast(`Request failed: ${errText}`, 'error');
          }
        } catch (err) {
          const duration = Math.round(performance.now() - startTime);
          resultBox.replaceChildren(
            h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, `Network error: ${err.message} (${duration}ms)`)
          );
          toast(`Network error: ${err.message}`, 'error');
        } finally {
          pingBtn.disabled = false;
          pingBtn.replaceChildren(icon('sparkles'), 'Test Live Ping');
        }
      }
    }, icon('sparkles'), 'Test Live Ping');

    // 2. Scope Preview Button
    const previewBtn = h('button', {
      class: 'btn btn-secondary',
      type: 'button',
      onclick: async () => {
        const selectedOpt = keySelect.selectedOptions?.[0];
        const keyID = selectedOpt?.getAttribute('data-id') || '';
        const model = modelInput.value.trim();

        previewBtn.disabled = true;
        try {
          const q = new URLSearchParams();
          if (keyID) q.set('key_id', keyID);
          if (model) q.set('model', model);
          const res = await api.get(`/plugins/resolve?${q.toString()}`);

          resultBox.style.display = 'block';
          resultBox.replaceChildren(
            h('div', { style: { marginBottom: '8px', fontWeight: 'bold', fontSize: '12.5px' } },
              `Scope Precedence for ${model || '(any model)'}:`
            ),
            h('table', { class: 'table', style: { width: '100%', fontSize: '12px' } },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Plugin'),
                  h('th', null, 'Effective State'),
                  h('th', null, 'Decided By'),
                  h('th', null, 'Overridden Candidates')
                )
              ),
              h('tbody', null,
                ...(res.plugins || []).map((rp) => {
                  const runs = rp.effective_state === 'on';
                  const overriddenText = (rp.overridden || []).map((o) => o.label).join(', ') || '—';
                  return h('tr', null,
                    h('td', null, h('b', null, rp.plugin.name)),
                    h('td', null, h('span', { class: `badge ${runs ? 'badge-primary' : ''}` }, runs ? 'ON' : 'OFF')),
                    h('td', null, rp.decided_by?.label || '—'),
                    h('td', { class: 'muted' }, overriddenText)
                  );
                })
              )
            )
          );
        } catch (e) {
          resultBox.style.display = 'block';
          resultBox.replaceChildren(h('p', { class: 'text-danger' }, e.message));
        } finally {
          previewBtn.disabled = false;
        }
      }
    }, icon('table'), 'Check Scope Preview');

    probeCard.replaceChildren(
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Live Plugin & Endpoint Probe'),
          h('p', { class: 'card-sub' }, 'Test the full pipeline (Agent → NineGuard Plugins → Upstream) and inspect transformed response style in real time.')
        )
      ),
      h('div', { style: { padding: '16px' } },
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' } },
          keySelect,
          modelInput,
          datalist,
          promptInput,
          pingBtn,
          previewBtn
        ),
        resultBox
      )
    );
  }

  // ── Reload data ──
  async function reload() {
    try {
      const [pRes, gRes, kRes, wRes, mRes] = await Promise.all([
        api.get('/plugins'),
        api.get('/model-groups'),
        api.get('/keys'),
        api.get('/plugins/warnings'),
        api.get('/models').catch(() => []),
      ]);
      pluginsList = pRes.plugins || [];
      groupsList = gRes.groups || [];
      keysList = kRes.keys || [];
      warningsList = wRes.warnings || [];
      modelsList = Array.isArray(mRes) ? mRes : (mRes?.models || []);

      if (!alive) return;
      render();
    } catch (e) {
      if (alive) toast(e.message, 'error');
    }
  }

  function render() {
    const banner = renderUpgradeBanner();
    const warnings = renderWarningsBanner();
    renderList();
    renderProbe();

    container.replaceChildren(
      header,
      banner || '',
      warnings || '',
      listEl,
      probeCard
    );
  }

  root.replaceChildren(container);
  reload();

  return {
    destroy() {
      alive = false;
    },
    update() {
      reload();
    }
  };
}
