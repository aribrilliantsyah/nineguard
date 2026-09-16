// Endpoints & Agent Setup: Upstream provider configuration, NineGuard API key generation, and IDE guides.
import { api } from '../api.js';
import { h, icon, toast, fmtNum, fmtCompact, emptyState, formDialog, confirmDialog } from '../ui.js';
import { setRoute } from '../state.js';

export function mount(root) {
  let alive = true;
  let activeTab = 'cursor';
  let modelsList = [];
  let keysList = [];
  let upstreamInfo = { configured: false, masked_key: '', key: '', router_target: 'http://localhost:20128' };

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
    const targetUrl = upstreamInfo?.router_target || 'http://localhost:20128';

    const statusBadge = isConfigured
      ? h('span', { class: 'badge ok', style: { padding: '4px 8px' } }, icon('check'), ' Provider Connected')
      : h('span', { class: 'badge warn', style: { padding: '4px 8px' } }, icon('alert'), ' Upstream Key Missing');

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
          h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase' } }, 'Target Route'),
          h('code', { style: { fontWeight: 'bold' } }, targetUrl),
          isConfigured
            ? h('span', { class: 'muted', style: { marginLeft: '12px', fontSize: '12px' } }, 'Master Key: ', h('code', null, upstreamInfo.masked_key))
            : null
        ),
        h('button', {
          class: 'btn btn-sm btn-primary',
          type: 'button',
          onclick: () => setRoute('providers')
        }, icon('server'), 'Manage Provider Settings')
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
      h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: openNewKeyModal }, icon('plus'), 'Generate New Key')
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

          return h('tr', null,
            h('td', { class: 'strong' },
              h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '8px' } }, icon('key')),
              k.name
            ),
            h('td', null, h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)),
            h('td', { class: 'num' }, fmtNum(k.total_requests || 0)),
            h('td', { class: 'num' }, fmtCompact(k.total_tokens || 0)),
            h('td', null, statusBtn),
            h('td', { style: { textAlign: 'right' } },
              h('div', { style: { display: 'inline-flex', gap: '6px' } },
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

  function openNewKeyModal() {
    const nameInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. Cursor IDE, Cline Mac, Pi Agent',
      value: 'Cursor IDE'
    });

    formDialog({
      title: 'Generate New NineGuard API Key',
      submitText: 'Generate Key',
      fields: [
        { label: 'Key Name / Description', input: nameInput },
        { node: h('p', { class: 'muted', style: { fontSize: '11.5px', marginTop: '4px' } },
          'This key will be used by your agent to authenticate against NineGuard. All request logs and token consumption will be grouped under this name.'
        )}
      ],
      onSubmit: async () => {
        const name = nameInput.value.trim() || 'Agent Key';
        try {
          const newKey = await api.post('/keys', { name });
          toast(`Key "${name}" created!`, 'ok');
          await load();

          // Show newly generated key prominently so user can copy
          showGeneratedKeyModal(newKey);
        } catch (e) {
          toast(`Failed to create key: ${e.message}`, 'error');
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
    const modelSelect = h('select', { class: 'select', style: { flex: '1' } });
    modelsList.filter(m => m.enabled).forEach(m => {
      modelSelect.append(h('option', { value: m.id }, m.id));
    });

    const keySelect = h('select', { class: 'select', style: { flex: '1' } });
    keysList.filter(k => k.is_active).forEach(k => {
      keySelect.append(h('option', { value: k.raw_key || k.key }, `${k.name} (${k.key})`));
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
      const [u, m, k] = await Promise.all([
        api.get('/settings/upstream').catch(() => ({})),
        api.get('/models').catch(() => []),
        api.get('/keys').catch(() => ({ keys: [] }))
      ]);
      upstreamInfo = u || {};
      modelsList = Array.isArray(m) ? m : [];
      keysList = (k && k.keys) ? k.keys : [];
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
