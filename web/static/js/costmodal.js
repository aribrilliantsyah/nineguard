import { api } from './api.js';
import { h, icon, toast, formDialog } from './ui.js';

export async function openCostSettingsModal(onSaved) {
  let costSettings = { effective_currency: 'USD', effective_rate: 1.0 };
  try {
    const s = await api.get('/settings/cost');
    if (s) costSettings = s;
  } catch {}

  const currencies = [
    { code: 'USD', name: 'USD — US Dollar ($)' },
    { code: 'IDR', name: 'IDR — Indonesian Rupiah (Rp)' },
    { code: 'EUR', name: 'EUR — Euro (€)' },
    { code: 'GBP', name: 'GBP — British Pound (£)' },
    { code: 'SGD', name: 'SGD — Singapore Dollar (S$)' },
    { code: 'JPY', name: 'JPY — Japanese Yen (¥)' },
    { code: 'AUD', name: 'AUD — Australian Dollar (A$)' },
    { code: 'CAD', name: 'CAD — Canadian Dollar (C$)' },
    { code: 'CNY', name: 'CNY — Chinese Yuan (¥)' },
    { code: 'INR', name: 'INR — Indian Rupee (₹)' }
  ];

  const currSelect = h('select', { class: 'input' },
    ...currencies.map(c => h('option', {
      value: c.code,
      selected: (costSettings.display_currency || 'USD').toUpperCase() === c.code
    }, c.name))
  );

  const manualRateInput = h('input', {
    class: 'input',
    type: 'number',
    step: 'any',
    placeholder: 'Automatic via Frankfurter / ECB',
    value: costSettings.fx_rate_manual || ''
  });

  const costVisibleCheck = h('input', {
    type: 'checkbox',
    checked: costSettings.cost_visible_to_keys !== false
  });

  const refreshPriceListBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    onclick: async () => {
      try {
        refreshPriceListBtn.disabled = true;
        const res = await api.post('/prices/refresh');
        toast(`Price list refreshed: ${res.count || 0} families synced`, 'ok');
      } catch (e) {
        toast(`Failed to refresh prices: ${e.message}`, 'error');
      } finally {
        refreshPriceListBtn.disabled = false;
      }
    }
  }, icon('refresh'), 'Refresh Price Catalog');

  formDialog({
    title: 'Currency & Cost Settings',
    submitText: 'Save Settings',
    fields: [
      {
        label: 'Display Currency',
        node: h('div', null,
          currSelect,
          h('p', { class: 'muted', style: { fontSize: '11.5px', margin: '4px 0 0' } },
            `Active conversion: 1 USD = ${costSettings.effective_currency} ${Number(costSettings.effective_rate || 1).toLocaleString()} (${costSettings.effective_source || 'usd'})`
          )
        )
      },
      {
        label: 'Manual FX Rate Override (Optional)',
        node: h('div', null,
          manualRateInput,
          h('p', { class: 'muted', style: { fontSize: '11.5px', margin: '4px 0 0' } },
            'Leave blank to automatically fetch daily rates from Frankfurter / European Central Bank.'
          )
        )
      },
      {
        label: 'Cost Visibility to Key Holders',
        node: h('label', { style: { display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '13px' } },
          costVisibleCheck,
          'Allow API keys to see estimated costs via /usage and /portal'
        )
      },
      {
        label: 'Price Catalog Sync',
        node: h('div', { style: { marginTop: '4px' } },
          refreshPriceListBtn
        )
      }
    ],
    onSubmit: async () => {
      await api.post('/settings/cost', {
        display_currency: currSelect.value,
        fx_rate_manual: manualRateInput.value.trim(),
        cost_visible_to_keys: costVisibleCheck.checked
      });
      toast('Cost and currency settings updated', 'ok');
      if (onSaved) onSaved();
    }
  });
}
