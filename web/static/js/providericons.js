import { h, icon } from './ui.js';

export const PROVIDER_ICONS = [['','Auto'],['9router','9router'],['omniroute','OmniRoute'],['nineguard','NineGuard'],['claude','Claude'],['anthropic','Anthropic'],['openai','OpenAI'],['openrouter','OpenRouter'],['ollama','Ollama'],['gemini','Gemini'],['generic','Generic'],['custom','Custom image URL']];

export function providerMark(provider) {
  const box = h('span', { class: 'provider-mark', 'aria-hidden': 'true' });
  const fallback = () => box.replaceChildren(h('span', null, (provider.name || 'P').slice(0, 2).toUpperCase()));
  let choice = provider.icon || (provider.family === 'codex' ? 'openai' : provider.family) || '';
  if (!choice) {
    const text = `${provider.name} ${provider.route} ${provider.quota_type}`.toLowerCase();
    choice = PROVIDER_ICONS.find(([id]) => id && !['custom', 'generic'].includes(id) && text.includes(id))?.[0] || 'generic';
  }
  const local = { nineguard: '/appicon.png', '9router': '/provider-icons/9router.svg', omniroute: '/provider-icons/omniroute.svg', claude: '/provider-icons/claude.svg', anthropic: '/provider-icons/anthropic.svg', openai: '/provider-icons/openai.svg', openrouter: '/provider-icons/openrouter.svg', ollama: '/provider-icons/ollama.svg', gemini: '/provider-icons/gemini.svg' };
  let src = local[choice];
  if (!src) { try { const u = new URL(choice); if (['https:', 'http:'].includes(u.protocol)) src = u.href; } catch {} }
  if (src) box.append(h('img', { src, alt: '', referrerpolicy: 'no-referrer', onerror: fallback }));
  else box.append(icon('server'));
  return box;
}

// 9router connection.provider ids that ship a mark under /provider-icons/9router/<id>.png.
export const SUB_PROVIDER_PNGS = new Set(['antigravity', 'codex', 'claude', 'github', 'copilot', 'kiro', 'qoder', 'cursor', 'gemini-cli', 'glm', 'kimi', 'minimax', 'deepseek', 'groq', 'xiaomi-mimo', 'zed', 'opencode', 'codebuddy-cn', 'commandcode', 'grok-cli']);
const SUB_PROVIDER_EXTRA = { nineguard: '/appicon.png', openai: '/provider-icons/openai.svg', anthropic: '/provider-icons/anthropic.svg', gemini: '/provider-icons/gemini.svg', google: '/provider-icons/gemini.svg' };
// Connection provider id -> icon URL, or '' when unknown (callers show initials).
export function subProviderIconURL(id) {
  const key = String(id || '').toLowerCase();
  if (SUB_PROVIDER_PNGS.has(key)) return `/provider-icons/9router/${key}.png`;
  return SUB_PROVIDER_EXTRA[key] || '';
}

export const initials = (label) => String(label || '?').replace(/[^A-Za-z0-9]/g, '').slice(0, 2).toUpperCase() || '?';

// Small square tile: spec is { img: url } | { ic: spriteName } | null (initials of label). A broken img falls back to initials.
export function iconTile(spec, label, size = '') {
  const el = h('span', { class: `uq-tile${size ? ` ${size}` : ''}`, 'aria-hidden': 'true' });
  const text = () => h('span', null, initials(label));
  if (spec?.img) el.append(h('img', { src: spec.img, alt: '', referrerpolicy: 'no-referrer', onerror: () => el.replaceChildren(text()) }));
  else if (spec?.ic) el.append(icon(spec.ic));
  else el.append(text());
  return el;
}

export const subProviderTile = (id, label, size) => { const img = subProviderIconURL(id); return iconTile(img ? { img } : null, label, size); };
