// Pure JS helpers for NineGuard plugin system: scope labels, precedence text, warning banners.

export function scopeLabel(scopeType, name = '') {
  switch (scopeType) {
    case 'global':
      return 'All keys, all models';
    case 'group':
      return `Models in "${name || 'Unnamed group'}"`;
    case 'key':
      return `Key "${name || 'Unnamed key'}"`;
    default:
      return name || scopeType;
  }
}

export function scopeSubLabel(scopeType, meta = {}) {
  switch (scopeType) {
    case 'global':
      return 'Default for every request. Groups and keys below can override it.';
    case 'group': {
      const modelsCount = meta.modelsCount || 0;
      const keysCount = meta.keysCount || 0;
      const base = `${modelsCount} models · any key`;
      if (keysCount > 0) {
        return `${base} · linked to ${keysCount} keys for access`;
      }
      return `${base} · plugin-only group`;
    }
    case 'key':
      return 'Any model this key uses';
    default:
      return '';
  }
}

export function formatInheritText(scopeType) {
  switch (scopeType) {
    case 'key':
      return 'Inherit (use group or All keys, all models)';
    case 'group':
      return 'Inherit (use All keys, all models)';
    default:
      return 'Inherit';
  }
}

export function sortPipeline(plugins) {
  return [...plugins].sort((a, b) => (a.pipeline_order || 0) - (b.pipeline_order || 0));
}

export function warningBannerText(count) {
  if (count === 1) {
    return '1 overlap warning active';
  }
  return `${count} overlap warnings active`;
}
