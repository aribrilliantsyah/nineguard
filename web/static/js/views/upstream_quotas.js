// Upstream Quotas view: Live visibility into upstream provider capacity, rolling limits, and credit balances.
import { api } from '../api.js';
import { h, icon, toast, emptyState, fmtNum, confirmDialog } from '../ui.js';
import { formatResetIn } from '../modelquotas.js';
import { formatTokensCompact } from '../quotahelpers.js';

export function upstreamBarTone(pct) {
  if (pct > 70) return 'ok';
  if (pct >= 30) return 'warn';
  return 'danger';
}

function timeAgo(dateStr) {
  if (!dateStr) return '';
  const diffSec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (diffSec < 60) return 'just now';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return `${Math.floor(diffSec / 86400)}d ago`;
}

export function renderUpstreamQuotas(container) {
  let alive = true;
  let loading = false;
  let providersData = [];

  const content = h('div', { class: 'mt' },
    h('div', { class: 'skel', style: { height: '140px', borderRadius: '8px' } })
  );

  // Auto-sync interval setting (global, applies to all providers)
  const intervalSel = h('select', { class: 'select', disabled: true });
  const intervalBar = h('div', {
    style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '4px' }
  },
    icon('refresh'),
    h('span', { style: { fontSize: '12.5px', fontWeight: '600' } }, 'Auto-sync every'),
    intervalSel,
    h('span', { class: 'muted', style: { fontSize: '11.5px' } }, 'Applies to the dashboard and the agent /quota command.')
  );
  container.replaceChildren(intervalBar, content);

  async function loadSyncSettings() {
    try {
      const s = await api.get('/settings/upstream-sync');
      if (!alive) return;
      intervalSel.replaceChildren(...(s.allowed || []).map((v) =>
        h('option', { value: v, selected: v === s.interval }, v)
      ));
      intervalSel.value = s.interval;
      intervalSel.disabled = false;
    } catch (err) {
      if (!alive) return;
      intervalBar.replaceChildren(
        h('span', { class: 'muted', style: { fontSize: '12px' } }, `Sync interval unavailable: ${err.message}`)
      );
    }
  }

  intervalSel.addEventListener('change', async () => {
    intervalSel.disabled = true;
    try {
      const s = await api.post('/settings/upstream-sync', { interval: intervalSel.value });
      toast(`Auto-sync interval set to ${s.interval}`, 'ok');
      intervalSel.value = s.interval;
    } catch (err) {
      toast(`Failed to save interval: ${err.message}`, 'error');
      await loadSyncSettings();
    } finally {
      intervalSel.disabled = false;
    }
  });

  async function load(force = false) {
    if (loading) return;
    loading = true;
    try {
      const url = force ? '/upstream-quotas?force=1' : '/upstream-quotas';
      const res = await api.get(url);
      if (!alive) return;
      providersData = res.providers || [];
      render();
    } catch (err) {
      if (!alive) return;
      content.replaceChildren(
        h('div', { class: 'banner err' }, icon('alert'), ` Failed to load upstream quotas: ${err.message}`)
      );
    } finally {
      loading = false;
    }
  }

  function renderBucketRow(b) {
    const tone = upstreamBarTone(b.remaining_percentage);
    const pct = Math.max(0, Math.min(100, b.remaining_percentage || 0));

    let usedTotalStr = `${formatTokensCompact(b.used)} / ${formatTokensCompact(b.total)}`;
    if (b.is_credit_balance) {
      usedTotalStr = `$${(b.remaining / 100).toFixed(2)}`;
    } else if (b.total <= 0) {
      usedTotalStr = '-';
    }

    const resetStr = b.reset_at ? `in ${formatResetIn(b.reset_at)}` : '-';

    return h('tr', null,
      h('td', { style: { fontWeight: '500' } },
        b.warning ? h('span', { title: 'Depletion warning (<25%)', style: { marginRight: '6px' } }, '⚠️') : null,
        b.name
      ),
      h('td', { class: 'muted', style: { fontSize: '12px' } }, usedTotalStr),
      h('td', null,
        b.is_credit_balance
          ? h('span', { class: 'badge ok' }, 'Active')
          : h('span', null,
              h('span', { class: `qbar ${tone}`, style: { width: '80px', display: 'inline-block' } },
                h('i', { style: { width: `${pct}%` } })
              ),
              h('span', { class: `qpct ${tone}`, style: { marginLeft: '8px', fontSize: '11px', fontWeight: 'bold' } }, `${pct}%`)
            )
      ),
      h('td', { class: 'muted', style: { fontSize: '12px' } }, resetStr)
    );
  }

  function renderConnectionCard(conn, provider) {
    const hasError = conn.status === 'error';
    const isKeyAllowance = (conn.connection_id || '').includes('allowance');

    const resetBtn = conn.reset_credits > 0 ? h('button', {
      class: 'badge badge-btn err',
      style: { display: 'inline-flex', alignItems: 'center', gap: '4px', cursor: 'pointer', fontWeight: 'bold' },
      type: 'button',
      title: `Use 1 reset now (${conn.reset_credits} available)`,
      onclick: async () => {
        const ok = await confirmDialog({
          title: 'Spend Upstream Quota Reset?',
          body: `Spend 1 irreversible reset grant for "${conn.account_label || conn.connection_id}"? This will refill session and weekly limits on ${provider.provider_name}. (${conn.reset_credits} resets available)`,
          danger: true,
          okText: 'Confirm & Reset'
        });
        if (!ok) return;
        try {
          resetBtn.disabled = true;
          resetBtn.textContent = 'Resetting...';
          const res = await api.post(`/upstream-quotas/${provider.provider_id}/connections/${conn.connection_id}/reset`, {
            provider: conn.provider,
            grant_id: conn.next_grant_id || ''
          });
          toast(res.message || 'Reset applied successfully', 'ok');
          await load(true);
        } catch (e) {
          toast(`Reset failed: ${e.message}`, 'error');
        } finally {
          resetBtn.disabled = false;
        }
      }
    }, icon('refresh'), ` ${conn.reset_credits} Reset`) : null;

    const isSharedPool = provider.quota_type === 'nineguard' || conn.plan === 'Shared Upstream Pool';
    const tierBadge = isKeyAllowance
      ? h('span', { class: 'badge ok', style: { fontWeight: 'bold', border: '1px solid var(--ok)' } }, '👤 Key Allowance (Admin Limit)')
      : (isSharedPool
          ? h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: 'bold', border: '1px solid var(--border)' } }, '🌐 Shared Upstream Pool')
          : null);

    const planBadge = (conn.plan && conn.plan !== 'Shared Upstream Pool')
      ? h('span', { class: 'badge' }, conn.plan)
      : null;

    return h('div', {
      style: {
        background: isKeyAllowance ? 'color-mix(in srgb, var(--ok) 5%, var(--panel))' : 'var(--panel)',
        border: isKeyAllowance ? '1.5px solid var(--ok)' : '1px solid var(--border)',
        borderRadius: '8px',
        padding: '12px 14px',
        marginBottom: '12px'
      }
    },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
          icon(isKeyAllowance ? 'shield' : 'user'),
          h('span', { style: { fontWeight: '700', fontSize: '13px' } }, conn.account_label || conn.connection_id),
          tierBadge,
          planBadge,
          resetBtn
        ),
        h('span', { class: `badge ${conn.status === 'ok' ? 'ok' : 'err'}` }, conn.status)
      ),
      hasError && conn.error_msg
        ? h('div', { class: 'banner err', style: { padding: '6px 10px', fontSize: '11.5px', marginBottom: '8px' } }, conn.error_msg)
        : null,
      conn.buckets && conn.buckets.length > 0
        ? h('table', { class: 'table', style: { width: '100%', fontSize: '12px', margin: 0 } },
            h('thead', null,
              h('tr', null,
                h('th', { style: { width: '35%' } }, isKeyAllowance ? 'Model / Allowance Window' : 'Window / Bucket'),
                h('th', { style: { width: '25%' } }, isKeyAllowance ? 'Tokens Consumed / Limit' : 'Used / Total'),
                h('th', { style: { width: '25%' } }, 'Remaining'),
                h('th', { style: { width: '15%' } }, 'Reset In')
              )
            ),
            h('tbody', null, ...conn.buckets.map(renderBucketRow))
          )
        : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, 'No active quota buckets reported.')
    );
  }

  function render() {
    const activeTrackers = providersData.filter((p) => p.quota_type && p.quota_type !== 'disabled');

    if (!providersData.length || !activeTrackers.length) {
      content.replaceChildren(
        emptyState(
          'data_usage',
          'No Upstream Quota Trackers Configured',
          'Configure a provider with 9router API or Cascaded NineGuard protocol in the Providers settings to track live upstream limits.',
          h('button', {
            class: 'btn btn-primary',
            type: 'button',
            style: { marginTop: '12px' },
            onclick: () => location.hash = '#/providers'
          }, icon('server'), 'Manage Providers')
        )
      );
      return;
    }

    const providerCards = providersData.map((p) => {
      const isUnsupported = p.quota_type === 'disabled';
      const refreshBtn = h('button', {
        class: 'btn btn-sm',
        type: 'button',
        disabled: isUnsupported || loading,
        title: 'Force refresh live quotas from upstream',
        onclick: async () => {
          refreshBtn.disabled = true;
          await load(true);
          toast(`Refreshed quotas for ${p.provider_name}`, 'ok');

          // 30-second cooldown timer
          let cooldown = 30;
          refreshBtn.disabled = true;
          refreshBtn.replaceChildren(icon('refresh'), ` Cooldown (${cooldown}s)`);
          const timer = setInterval(() => {
            cooldown--;
            if (cooldown <= 0 || !alive) {
              clearInterval(timer);
              refreshBtn.disabled = false;
              refreshBtn.replaceChildren(icon('refresh'), 'Refresh');
            } else {
              refreshBtn.replaceChildren(icon('refresh'), ` Cooldown (${cooldown}s)`);
            }
          }, 1000);
        }
      }, icon('refresh'), 'Refresh');

      const protocolLabel = p.quota_type === '9router'
        ? 'Router Protocol'
        : p.quota_type === 'nineguard'
        ? 'NineGuard Cascaded'
        : 'Direct LLM (No Tracker)';

      const lastUpdatedLabel = p.last_updated
        ? h('span', { class: 'muted', style: { fontSize: '11.5px', marginLeft: '6px' } }, `Synced ${timeAgo(p.last_updated)}`)
        : null;

      return h('div', { class: 'card', style: { marginBottom: '16px' } },
        h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
            icon('server'),
            h('h2', { style: { margin: 0, fontSize: '15px' } }, p.provider_name),
            h('span', { class: 'badge', style: { background: 'var(--hover)' } }, protocolLabel),
            h('span', { class: `badge ${p.status === 'ok' ? 'ok' : p.status === 'degraded' ? 'warn' : 'muted'}` }, p.status),
            lastUpdatedLabel
          ),
          refreshBtn
        ),
        p.error_msg
          ? h('div', { class: 'banner err', style: { margin: '8px 14px' } }, icon('alert'), ' ', p.error_msg)
          : null,
        h('div', { style: { padding: '12px 14px' } },
          isUnsupported
            ? h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } },
                'This provider connects directly to an LLM without upstream limit telemetry. Switch protocol in Providers menu to enable.'
              )
            : p.connections && p.connections.length > 0
            ? h('div', null, ...p.connections.map((c) => renderConnectionCard(c, p)))
            : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, 'No account connections found on upstream.')
        )
      );
    });

    content.replaceChildren(...providerCards);
  }

  loadSyncSettings();
  load(false);

  return () => {
    alive = false;
  };
}
