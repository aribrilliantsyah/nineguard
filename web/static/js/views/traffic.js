// Traffic Logs (Explorer): Detailed real-time tracking of requests and token usage.
import { api } from '../api.js';
import { h, icon, toast, emptyState, fmtNum, fmtCompact, formDialog } from '../ui.js';

export function mount(root) {
  let alive = true;
  let timer = null;
  let live = false;

  let period = 'today';
  let status = '';
  let selectedKey = '';
  let queryText = '';
  let offset = 0;
  const limit = 50;

  let availableKeys = [];

  const tableCard = h('div', { class: 'card table-card' });
  const footerBar = h('div', { class: 'table-foot', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } });

  const searchInput = h('input', {
    class: 'input',
    type: 'text',
    placeholder: 'Search model, IP, or key...',
    spellcheck: 'false',
    style: { width: '200px' },
    oninput: (e) => {
      queryText = e.target.value.trim();
      offset = 0;
      load();
    }
  });

  const periodSelect = h('select', {
    class: 'select',
    onchange: (e) => {
      period = e.target.value;
      offset = 0;
      load();
    }
  },
    h('option', { value: 'today' }, 'Today'),
    h('option', { value: '7d' }, 'Last 7 days'),
    h('option', { value: '30d' }, 'Last 30 days')
  );

  const statusSelect = h('select', {
    class: 'select',
    onchange: (e) => {
      status = e.target.value;
      offset = 0;
      load();
    }
  },
    h('option', { value: '' }, 'All Status'),
    h('option', { value: 'ok' }, 'OK (200)'),
    h('option', { value: 'blocked' }, 'Blocked (403)'),
    h('option', { value: 'error' }, 'Errors')
  );

  const keySelect = h('select', {
    class: 'select',
    style: { maxWidth: '170px' },
    onchange: (e) => {
      selectedKey = e.target.value;
      offset = 0;
      load();
    }
  },
    h('option', { value: '' }, 'All Client Keys')
  );

  const liveBtn = h('button', {
    class: 'chip live',
    type: 'button',
    onclick: () => toggleLive()
  }, h('span', { class: 'dot' }), 'Live tail');

  const exportBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    onclick: () => exportCSV()
  }, icon('download'), 'Export');

  root.append(h('div', { class: 'page fill' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Traffic Explorer'),
        h('p', null, 'Real-time trace of API keys, model routing, duration, and token usage')
      ),
      h('div', { class: 'page-actions' },
        searchInput,
        keySelect,
        statusSelect,
        periodSelect,
        liveBtn,
        exportBtn
      )
    ),
    tableCard,
    footerBar
  ));

  function toggleLive() {
    live = !live;
    liveBtn.classList.toggle('active', live);
    if (live) {
      timer = setInterval(load, 3000);
    } else {
      clearInterval(timer);
    }
  }

  function formatTime(isoStr) {
    if (!isoStr) return '-';
    const d = new Date(isoStr);
    return d.toTimeString().split(' ')[0] + '.' + String(d.getMilliseconds()).padStart(3, '0');
  }

  function formatDuration(ms) {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
  }

  function showDetails(log) {
    const copyBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      onclick: () => {
        navigator.clipboard.writeText(JSON.stringify(log, null, 2)).then(() => {
          toast('Copied log JSON to clipboard', 'ok');
        });
      }
    }, icon('copy'), 'Copy JSON');

    formDialog({
      title: `Request Details #${log.id}`,
      submitText: 'Close',
      cancel: false,
      fields: [
        { node: h('div', { class: 'kv-grid' },
          h('div', null, h('div', { class: 'k' }, 'Timestamp'), h('div', { class: 'v' }, new Date(log.timestamp).toLocaleString())),
          h('div', null, h('div', { class: 'k' }, 'Model'), h('div', { class: 'v strong' }, log.model)),
          h('div', null, h('div', { class: 'k' }, 'Client Key Name'), h('div', { class: 'v' }, log.api_key_name || '(Default Key)')),
          h('div', null, h('div', { class: 'k' }, 'Masked Key'), h('div', { class: 'v' }, h('code', null, log.api_key))),
          h('div', null, h('div', { class: 'k' }, 'Status'), h('div', { class: 'v' },
            log.status_code === 403
              ? h('span', { class: 'badge err' }, '403 BLOCKED (Model Policy)')
              : log.status_code >= 400
                ? h('span', { class: 'badge err' }, `${log.status_code} ERROR`)
                : h('span', { class: 'badge ok' }, `${log.status_code} OK`)
          )),
          h('div', null, h('div', { class: 'k' }, 'Latency'), h('div', { class: 'v' }, formatDuration(log.duration_ms))),
          h('div', null, h('div', { class: 'k' }, 'Prompt Tokens'), h('div', { class: 'v' }, fmtNum(log.prompt_tokens))),
          h('div', null, h('div', { class: 'k' }, 'Completion Tokens'), h('div', { class: 'v' }, fmtNum(log.completion_tokens))),
          h('div', null, h('div', { class: 'k' }, 'Total Tokens'), h('div', { class: 'v strong' }, fmtNum(log.total_tokens))),
          h('div', null, h('div', { class: 'k' }, 'Stream Mode'), h('div', { class: 'v' }, log.stream ? 'Server-Sent Events (SSE)' : 'Synchronous JSON')),
          h('div', null, h('div', { class: 'k' }, 'Client IP'), h('div', { class: 'v' }, log.client_ip || '-')),
        )},
        log.error_message ? { node: h('div', { class: 'note warn mt-sm' }, icon('alert'), h('b', null, 'Error: '), log.error_message) } : null,
        { node: h('div', { style: { display: 'flex', justifyContent: 'flex-end', marginTop: '12px' } }, copyBtn) }
      ].filter(Boolean)
    });
  }

  function exportCSV() {
    api.download('/traffic?export=csv', {
      period,
      status,
      model: queryText,
      api_key: selectedKey
    });
  }

  async function loadKeys() {
    try {
      const res = await api.get('/keys');
      if (res && res.keys) {
        availableKeys = res.keys;
        keySelect.replaceChildren(
          h('option', { value: '' }, 'All Client Keys'),
          ...availableKeys.map(k => h('option', { value: k.name || k.key }, k.name ? `${k.name} (${k.key})` : k.key))
        );
      }
    } catch {
      // ignore
    }
  }

  async function load() {
    let res;
    try {
      const apiKeyFilter = selectedKey || queryText;
      res = await api.get('/traffic', {
        period,
        status,
        model: queryText,
        api_key: apiKeyFilter,
        limit,
        offset
      });
    } catch (e) {
      if (alive) tableCard.replaceChildren(emptyState('alert', 'Could not fetch traffic logs', e.message));
      return;
    }
    if (!alive) return;

    const logs = res.logs || [];
    const total = res.total || 0;

    if (!logs.length) {
      tableCard.replaceChildren(emptyState('inbox', 'No traffic recorded yet', 'Incoming requests sent to NineGuard (/v1/*) will appear here in real time.'));
      footerBar.replaceChildren();
      return;
    }

    const rows = logs.map((l) => {
      let statusBadge = h('span', { class: 'badge ok' }, '200 OK');
      if (l.status_code === 403) {
        statusBadge = h('span', { class: 'badge err' }, 'BLOCKED');
      } else if (l.status_code >= 400) {
        statusBadge = h('span', { class: 'badge err' }, `${l.status_code} ERR`);
      }

      const keyDisplay = l.api_key_name
        ? h('div', null,
            h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: '600', color: 'var(--text)', whiteSpace: 'nowrap' } },
              icon('key'), ' ', l.api_key_name
            ),
            h('div', { class: 'muted', style: { fontSize: '11px', marginTop: '2px', fontVariantNumeric: 'tabular-nums' } }, l.api_key)
          )
        : h('code', { class: 'muted' }, l.api_key);

      const tr = h('tr', {
        style: { cursor: 'pointer' },
        onclick: () => showDetails(l)
      },
        h('td', { class: 'muted', style: { fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' } }, formatTime(l.timestamp)),
        h('td', null, statusBadge),
        h('td', { class: 'strong' }, l.model),
        h('td', null, keyDisplay),
        h('td', { class: 'num' }, fmtNum(l.prompt_tokens)),
        h('td', { class: 'num' }, fmtNum(l.completion_tokens)),
        h('td', { class: 'num strong' }, fmtNum(l.total_tokens)),
        h('td', { class: 'num muted' }, formatDuration(l.duration_ms)),
        h('td', { class: 'muted' }, l.stream ? 'Stream' : 'Sync'),
        h('td', { class: 'muted' }, l.client_ip || '-')
      );
      return tr;
    });

    tableCard.replaceChildren(
      h('div', { class: 'table-wrap' },
        h('table', { class: 'table clickable' },
          h('thead', null,
            h('tr', null,
              h('th', null, 'Time'),
              h('th', null, 'Status'),
              h('th', null, 'Model'),
              h('th', null, 'Client Key'),
              h('th', { class: 'num' }, 'Prompt Tok'),
              h('th', { class: 'num' }, 'Comp Tok'),
              h('th', { class: 'num' }, 'Total Tok'),
              h('th', { class: 'num' }, 'Latency'),
              h('th', null, 'Mode'),
              h('th', null, 'Client IP')
            )
          ),
          h('tbody', null, ...rows)
        )
      )
    );

    // Footer info & pagination
    const startIdx = offset + 1;
    const endIdx = Math.min(offset + limit, total);
    const prevBtn = h('button', {
      class: 'btn btn-sm',
      disabled: offset === 0,
      onclick: () => { offset = Math.max(0, offset - limit); load(); }
    }, 'Previous');

    const nextBtn = h('button', {
      class: 'btn btn-sm',
      disabled: endIdx >= total,
      onclick: () => { offset += limit; load(); }
    }, 'Next');

    footerBar.replaceChildren(
      h('span', null, `Showing ${startIdx}–${endIdx} of ${fmtNum(total)} requests`),
      h('div', { style: { display: 'flex', gap: '6px' } }, prevBtn, nextBtn)
    );
  }

  loadKeys();
  load();

  return {
    update() {},
    refresh: () => { loadKeys(); load(); },
    destroy() {
      alive = false;
      if (timer) clearInterval(timer);
    }
  };
}
