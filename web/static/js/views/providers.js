// Provider connections: custom upstream gateways and embedded OAuth accounts.
import { api } from '../api.js';
import { h, icon, toast, emptyState, formDialog, confirmDialog, passwordField, fmtDateTime, menu } from '../ui.js';
import { PROVIDER_ICONS, providerMark } from '../providericons.js';
import { openProviderLogin } from '../provideroauth.js';
import { openCostSettingsModal } from '../costmodal.js';

// Anthropic API keys expose no quota endpoint; usage lives in the Claude Console.
export const ANTHROPIC_CONSOLE_USAGE_URL = 'https://platform.claude.com/settings/usage';
export const isAnthropicAPI = (route) => String(route || '').toLowerCase().includes('api.anthropic.com');

export function mount(root) {
  let alive = true;
  let providers = [];
  let connectors = [];
  let query = '';
  const body = h('div', { class: 'provider-list', 'aria-busy': 'true' },
    Array.from({ length: 3 }, () => h('div', { class: 'skel provider-skel', 'aria-hidden': 'true' })));
  const search = h('input', { type: 'search', placeholder: 'Find a provider...', 'aria-label': 'Find a provider', oninput: (e) => { query = e.target.value.toLowerCase().trim(); render(); } });
  const configBtn = h('button', { class: 'btn', type: 'button', 'aria-haspopup': 'menu', onclick: () => menu(configBtn, [
    { icon: 'dollar-sign', text: 'Cost & currency', onClick: () => openCostSettingsModal() },
  ]) }, icon('settings'), 'Configure');
  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', null, h('h1', null, 'Providers'), h('p', null, 'Connect an account or route requests through your own upstream gateway.')),
      h('div', { class: 'page-actions' }, configBtn,
        h('button', { class: 'btn btn-primary', type: 'button', onclick: () => edit() }, icon('plus'), 'Add provider'))),
    h('section', { class: 'provider-routing', 'aria-label': 'How routing works' },
      h('div', { class: 'provider-route-example', 'aria-hidden': 'true' },
        h('code', null, h('span', { class: 'route-prefix' }, 'openrouter'), '/', h('span', { class: 'route-model' }, 'anthropic/claude-3.5-sonnet'))),
      h('ul', { class: 'provider-route-rules' },
        h('li', null, h('b', null, 'Prefix picks the provider. '), 'NineGuard removes it before forwarding, so the upstream receives only the model id.'),
        h('li', null, h('b', null, 'No prefix? '), 'The request goes to the default provider.'),
        h('li', null, h('b', null, 'Protocol. '), 'Connected Claude and Codex accounts run through the built-in engine; custom URLs keep their own protocol.'))),
    h('div', { class: 'provider-toolbar' }, h('label', { class: 'tool-search' }, icon('search'), search)),
    body
  ));
  async function action(fn) { try { await fn(); await load(); } catch (err) { toast(err.message, 'error'); } }
  function render() {
    const list = providers.filter(p => `${p.name} ${p.prefix} ${p.family || ''} ${p.route}`.toLowerCase().includes(query));
    body.removeAttribute('aria-busy');
    if (!list.length) {
      body.replaceChildren(providers.length
        ? emptyState('search', 'No matching providers', 'Try another provider name or prefix.',
          h('button', { class: 'btn', type: 'button', onclick: () => { search.value = ''; query = ''; render(); search.focus(); } }, icon('x'), 'Clear search'))
        : emptyState('server', 'No providers connected', 'Add a custom URL or connect a Claude / Codex account.',
          h('button', { class: 'btn btn-primary', type: 'button', onclick: () => edit() }, icon('plus'), 'Add provider')));
      return;
    }
    body.replaceChildren(...list.map(p => {
      const sdk = p.backend === 'cliproxy';
      const accountsBody = h('div', { class: 'provider-accounts', hidden: !sdk });
      const probe = h('p', { class: 'provider-probe', role: 'status', hidden: true });
      const test = h('button', { class: 'btn btn-sm', type: 'button', onclick: async () => {
        test.disabled = true; probe.hidden = false; probe.textContent = 'Checking connection...';
        try {
          if (sdk) { const r = await api.get(`/providers/${p.id}/accounts`); probe.textContent = `${r.accounts.length} connected account(s). Send a model request to verify inference.`; }
          else { const r = await api.post('/providers/test', { id: p.id, route: p.route }); probe.textContent = r.ok && r.status >= 200 && r.status < 300 ? `Connected: ${r.models_count || 0} models, ${r.latency_ms} ms.` : (r.error || `Upstream returned HTTP ${r.status}.`); }
        } catch (err) { probe.textContent = err.message; } finally { test.disabled = false; }
      } }, icon('refresh'), 'Check connection');
      const controls = h('div', { class: 'provider-actions' },
        sdk ? h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: () => openProviderLogin(p, null, load) }, icon('plus'), 'Connect account') : test,
        h('button', { class: 'btn btn-sm', type: 'button', onclick: () => edit(p) }, icon('pencil'), 'Edit'),
        h('button', { class: 'icon-btn', type: 'button', 'aria-label': `Delete ${p.name}`, title: 'Delete provider', onclick: async () => {
          if (await confirmDialog({ title: `Delete ${p.name}?`, body: 'This removes its route and model catalog. Disconnect its OAuth accounts first. Historical traffic is kept.', danger: true, confirmText: 'Delete provider' })) action(() => api.del(`/providers/${p.id}`));
        } }, icon('trash'))
      );
      const node = h('section', { class: `provider-row ${p.is_active ? '' : 'provider-off'}` },
        h('div', { class: 'provider-identity' }, providerMark(p), h('div', { class: 'provider-name' }, h('h2', null, p.name), h('div', { class: 'provider-meta' }, h('code', null, p.prefix ? `${p.prefix}/` : 'root'), h('span', null, sdk ? `${connectors.find(c => c.id === p.family)?.label || p.family} OAuth` : 'Custom URL'), p.is_default ? h('span', { class: 'badge' }, 'Default') : null))),
        controls,
        h('div', { class: 'provider-connection' },
          h('div', null, h('span', { class: 'muted' }, sdk ? 'Authentication' : 'Upstream URL'), h('code', null, sdk ? 'Encrypted account credentials; automatic refresh' : p.route)),
          !sdk ? h('div', null, h('span', { class: 'muted' }, 'API key'), h('code', null, p.masked_key || 'Not required')) : null,
          h('div', { class: 'provider-actions' },
            h('button', { class: `badge badge-btn ${p.is_active ? 'ok' : 'muted'}`, type: 'button', onclick: () => action(() => api.post(`/providers/${p.id}/toggle`, { active: !p.is_active })) }, p.is_active ? 'Enabled' : 'Disabled'),
            !p.is_default ? h('button', { class: 'btn btn-sm', type: 'button', onclick: () => action(() => api.post(`/providers/${p.id}/default`)) }, 'Make default') : null,
            p.upstream_token_saving ? h('span', { class: 'badge warn', title: p.upstream_token_saving_note }, 'Upstream token saving') : null,
            sdk ? h('span', { class: 'badge' }, 'Automatic SDK telemetry')
              : isAnthropicAPI(p.route) ? h('a', { class: 'badge', href: ANTHROPIC_CONSOLE_USAGE_URL, target: '_blank', rel: 'noopener noreferrer', title: 'Anthropic API keys expose no quota telemetry' }, 'Usage in Claude Console')
              : p.quota_type !== 'disabled' ? h('span', { class: 'badge' }, `${p.quota_type} capacity`) : null
          )
        ), probe, accountsBody
      );
      if (sdk) {
        accountsBody.append(h('p', { class: 'muted' }, 'Loading accounts...'));
        api.get(`/providers/${p.id}/accounts`).then(res => {
          if (!alive || !node.isConnected) return;
          const accounts = res.accounts || [];
          accountsBody.replaceChildren(...(accounts.length ? accounts.map(a => h('div', { class: 'provider-account' },
            h('div', { class: 'provider-account-name' }, h('strong', null, a.label), h('span', { class: 'muted' }, a.expires_at ? `Token expires ${fmtDateTime(Date.parse(a.expires_at))}` : 'Expiry available after login'), a.last_refreshed_at ? h('span', { class: 'muted' }, `Last refreshed ${fmtDateTime(Date.parse(a.last_refreshed_at))}`) : null),
            h('span', { class: `badge ${a.status === 'connected' ? 'ok' : 'warn'}`, title: a.last_error || '' }, a.status.replaceAll('_', ' ')),
            h('div', { class: 'provider-actions' },
              h('button', { class: 'btn btn-sm', type: 'button', onclick: () => action(() => api.post(`/providers/${p.id}/accounts/${a.id}/refresh`, {})) }, 'Refresh token'),
              h('button', { class: 'btn btn-sm', type: 'button', onclick: () => openProviderLogin(p, a, load) }, 'Login again'),
              h('button', { class: 'btn btn-sm', type: 'button', onclick: () => action(() => api.patch(`/providers/${p.id}/accounts/${a.id}`, { disabled: a.status !== 'disabled' })) }, a.status === 'disabled' ? 'Enable' : 'Disable'),
              h('button', { class: 'btn btn-sm btn-danger', type: 'button', onclick: async () => { if (await confirmDialog({ title: 'Disconnect account?', body: 'Forgets the encrypted credential in NineGuard. This does not necessarily revoke access at the provider.', confirmText: 'Disconnect', danger: true })) action(() => api.del(`/providers/${p.id}/accounts/${a.id}`)); } }, 'Disconnect')
            )
          )) : [h('p', { class: 'muted' }, 'No account connected. Connect one to make models available.')]));
        }).catch(err => { accountsBody.replaceChildren(h('p', { class: 'muted' }, err.message)); });
      }
      return node;
    }));
  }

  function edit(existing) {
    const name = h('input', { class: 'input', value: existing?.name || '', placeholder: 'Provider name', 'aria-label': 'Provider name' });
    const prefix = h('input', { class: 'input', value: existing?.prefix || '', placeholder: 'e.g. claude, codex, router', 'aria-label': 'Routing prefix' });
    const type = h('select', { class: 'select wide', 'aria-label': 'Connection type', disabled: !!existing }, h('option', { value: 'http' }, 'Custom URL (OpenAI-compatible)'), ...connectors.map(c => h('option', { value: c.id }, `${c.label} — OAuth`)));
    type.value = existing?.backend === 'cliproxy' ? existing.family : 'http';
    const route = h('input', { class: 'input', value: existing?.route || '', type: 'url', placeholder: 'https://gateway.example/v1', 'aria-label': 'Upstream URL' });
    const key = passwordField({ placeholder: existing ? 'Leave empty to keep current key' : 'Upstream API key (if required)' });
    const customIcon = /^https?:\/\//.test(existing?.icon || '');
    const iconSelect = h('select', { class: 'select wide', 'aria-label': 'Provider icon' }, ...PROVIDER_ICONS.map(([id, label]) => h('option', { value: id }, label))); iconSelect.value = customIcon ? 'custom' : (existing?.icon || '');
    const iconURL = h('input', { class: 'input', type: 'url', value: customIcon ? existing.icon : '', placeholder: 'https://example.com/logo.png', 'aria-label': 'Custom icon URL', hidden: !customIcon });
    const preview = h('div', { class: 'provider-icon-preview' });
    const paint = () => { iconURL.hidden = iconSelect.value !== 'custom'; preview.replaceChildren(providerMark({ name: name.value, family: type.value === 'http' ? '' : type.value, route: route.value, icon: iconSelect.value === 'custom' ? iconURL.value : iconSelect.value })); };
    iconSelect.addEventListener('change', paint); iconURL.addEventListener('input', paint); name.addEventListener('input', paint);
    const def = h('input', { type: 'checkbox', checked: existing?.is_default || !providers.length });
    const quota = h('select', { class: 'select wide', 'aria-label': 'Capacity tracker' }, ...[['disabled','No capacity tracker'],['9router','9router / OmniRoute protocol'],['nineguard','Upstream NineGuard']].map(([v,l]) => h('option', { value:v },l))); quota.value = existing?.quota_type || 'disabled';
    const password = passwordField({ placeholder: 'Router dashboard password (leave empty to keep)' });
    const saving = h('input', { type: 'checkbox', checked: existing?.upstream_token_saving });
    const savingNote = h('input', { class: 'input', value: existing?.upstream_token_saving_note || '', maxlength: '200', placeholder: 'Optional note about upstream token saving' });
    const field = (label, node, hint) => h('label', { class: 'field' }, h('span', null, label), node, hint ? h('small', { class: 'muted' }, hint) : null);
    const trackerFields = h('div', { class: 'provider-form-stack' }, field('Capacity tracker', quota), field('Router dashboard password', password.wrap));
    const sdkTelemetryHint = h('p', { class: 'note' }, 'Automatic SDK telemetry. Limit percentages and reset times come from upstream response headers; Refresh reads the latest snapshot without sending a prompt.');
    const anthropicHint = h('p', { class: 'note' }, 'Anthropic API keys expose no quota telemetry, so no capacity tracker is used. Check usage, spend and rate limits in your own ', h('a', { href: ANTHROPIC_CONSOLE_USAGE_URL, target: '_blank', rel: 'noopener noreferrer' }, 'Claude Console'), '.');
    const httpFields = h('div', { class: 'provider-form-stack' }, field('Upstream URL', route), field('Upstream API key', key.wrap));
    const oauthHint = h('p', { class: 'note', hidden: type.value === 'http' }, 'Save this provider, then use Connect account. OAuth tokens are encrypted and refreshed by NineGuard.');
    const change = () => { const sdk = type.value !== 'http'; const anthropic = !sdk && isAnthropicAPI(route.value); httpFields.hidden = sdk; oauthHint.hidden = !sdk; trackerFields.hidden = sdk || anthropic; anthropicHint.hidden = !anthropic; sdkTelemetryHint.hidden = !sdk; paint(); }; type.addEventListener('change', change); route.addEventListener('input', change); change();
    formDialog({ title: existing ? `Edit ${existing.name}` : 'Add provider', submitText: existing ? 'Save changes' : 'Add provider', extraWide: true,
      fields: [{ node: h('div', { class: 'provider-form' },
        h('div', { class: 'provider-form-stack' }, field('Connection type', type), field('Provider name', name), field('Routing prefix', prefix, 'Models appear as prefix/model-id. Leave empty for the root provider.'), httpFields, oauthHint, h('label', { class: 'provider-check' }, def, 'Default for models without a prefix')),
        h('div', { class: 'provider-form-stack' }, field('Provider icon', h('div', null, h('div', { class: 'provider-icon-control' }, preview, iconSelect), iconURL), 'Custom image hosts receive browser requests; no referrer is sent.'),
          h('details', { class: 'provider-advanced' }, h('summary', null, 'Advanced settings'), trackerFields, anthropicHint, sdkTelemetryHint, h('label', { class: 'provider-check' }, saving, 'Upstream already saves tokens'), savingNote))
      ) }], onSubmit: async () => {
        if (!name.value.trim()) throw Error('Provider name is required');
        const sdk = type.value !== 'http';
        const payload = { name:name.value.trim(),prefix:prefix.value.trim(),route:sdk ? '' : route.value.trim(),backend:sdk ? 'cliproxy' : 'http',family:sdk ? type.value : '',icon:iconSelect.value === 'custom' ? iconURL.value.trim() : iconSelect.value,is_default:def.checked,is_active:existing?.is_active ?? true,quota_type:!sdk && isAnthropicAPI(route.value) ? 'disabled' : quota.value,upstream_token_saving:saving.checked,upstream_token_saving_note:savingNote.value.trim() };
        if (!sdk && (!existing || key.input.value.trim())) payload.api_key = key.input.value.trim();
        if (password.input.value.trim()) payload.quota_auth_secret = password.input.value.trim();
        if (existing) await api.put(`/providers/${existing.id}`, payload); else await api.post('/providers',payload);
        toast(existing ? 'Provider updated' : 'Provider added');await load();
      }
    });
  }
  async function load() {
    try { const [p,c] = await Promise.all([api.get('/providers'),api.get('/provider-connectors').catch(() => ({connectors:[]}))]); if (!alive) return;providers = p.providers || [];connectors = c.connectors || [];render(); }
    catch(err) { if (alive) { body.removeAttribute('aria-busy'); body.replaceChildren(emptyState('alert','Could not load providers',err.message,h('button',{class:'btn',onclick:load},'Try again'))); } }
  }
  load();
  return { update(){},refresh:load,destroy(){alive=false;} };
}
