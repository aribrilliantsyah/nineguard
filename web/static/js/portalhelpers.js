// Client Portal helpers for key validation, masking, and storage

const STORAGE_KEY = 'nineguard_client_key';

export function normalizeKey(key) {
  if (typeof key !== 'string') return '';
  let k = key.trim();
  if (k.toLowerCase().startsWith('bearer ')) {
    k = k.slice(7).trim();
  }
  return k;
}

export function isValidKeyFormat(key) {
  const k = normalizeKey(key);
  if (!k) return false;
  return k.startsWith('sk-');
}

export function maskKey(key) {
  const k = normalizeKey(key);
  if (!k) return '-';
  if (k.length <= 10) {
    return 'sk-...' + k.slice(-2);
  }
  // If key starts with sk-ng-, keep prefix
  if (k.startsWith('sk-ng-')) {
    return 'sk-ng-...' + k.slice(-4);
  }
  return k.slice(0, 6) + '...' + k.slice(-4);
}

export function buildHeaders(key) {
  const k = normalizeKey(key);
  return {
    'Authorization': 'Bearer ' + k
  };
}

export function getStoredPortalKey(storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return '';
  try {
    return normalizeKey(storage.getItem(STORAGE_KEY) || '');
  } catch {
    return '';
  }
}

export function setStoredPortalKey(key, storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return;
  try {
    const k = normalizeKey(key);
    if (k) {
      storage.setItem(STORAGE_KEY, k);
    } else {
      storage.removeItem(STORAGE_KEY);
    }
  } catch { /* storage unavailable */ }
}

export function clearStoredPortalKey(storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return;
  try {
    storage.removeItem(STORAGE_KEY);
  } catch { /* storage unavailable */ }
}
