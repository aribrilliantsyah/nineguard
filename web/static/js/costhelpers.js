// Cost & Currency formatting helpers per ADR 0012 and Spec §5.4

export function formatCost(costUSD, currency = 'USD', fxRate = 1.0, unpricedCount = 0) {
  if (costUSD == null || isNaN(costUSD)) {
    return '—';
  }

  const prefix = unpricedCount > 0 ? '≥ ' : '';
  const cur = (currency || 'USD').toUpperCase();
  const rate = (fxRate > 0) ? fxRate : 1.0;
  const converted = costUSD * rate;

  // Sub-unit thresholds
  if (costUSD > 0) {
    if (cur === 'IDR' && converted < 1.0) {
      return `${prefix}<Rp 1`;
    } else if (cur !== 'IDR' && costUSD < 0.01) {
      let sym = '$';
      if (cur === 'EUR') sym = '€';
      else if (cur === 'JPY') sym = '¥';
      else if (cur === 'SGD') sym = 'S$';
      else if (cur !== 'USD') sym = `${cur} `;
      return `${prefix}<${sym}0.01`;
    }
  }

  try {
    const isZeroDecimal = (cur === 'IDR' || cur === 'JPY' || cur === 'KRW');
    const nf = new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: cur,
      minimumFractionDigits: isZeroDecimal ? 0 : 2,
      maximumFractionDigits: isZeroDecimal ? 0 : 2
    });
    return prefix + nf.format(converted);
  } catch {
    return `${prefix}${cur} ${converted.toFixed(2)}`;
  }
}

export function costTooltip(costUSD, currency = 'USD', fxRate = 1.0, syncDate = '') {
  let tip = 'API list-price equivalent, not actual spend';
  const cur = (currency || 'USD').toUpperCase();
  if (cur !== 'USD' && fxRate > 0) {
    const rateStr = Number(fxRate).toLocaleString('en-US', { maximumFractionDigits: 4 });
    const dateStr = syncDate ? `ECB, ${syncDate}` : 'ECB';
    tip += `\n1 USD = ${rateStr} (${dateStr}) · converted at today's rate`;
  }
  return tip;
}

export function formatPriceRate(inputPerMTok, outputPerMTok) {
  if (inputPerMTok == null || outputPerMTok == null) {
    return 'No price';
  }
  return `$${Number(inputPerMTok).toFixed(2)} / $${Number(outputPerMTok).toFixed(2)} per 1M`;
}
