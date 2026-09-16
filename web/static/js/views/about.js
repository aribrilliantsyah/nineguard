// About: why the app exists, what it runs on, and who built it.
// Everything shown here lives in the constants below.
import { h, icon } from '../ui.js';
import { store } from '../state.js';

const APP = 'NineGuard';
const TAGLINE = 'Proxy & Firewall Gateway for OpenAI-Compatible Providers';
const DESCRIPTION = 'A lightweight middleware and firewall gateway for OpenAI-compatible providers that enables robust model blocking, multi-provider routing with custom prefixes, and precise traffic logging per API key. It intercepts requests before they reach upstream providers, blocking disabled models instantly and tracking token usage efficiently.';
const LICENSE = 'MIT license';

const AUTHOR = {
  name: 'Ari Ardiansyah',
  github: 'aribrilliantsyah',
  email: 'ariadiansyah.study@gmail.com',
};

// Why the app exists: what breaks without it, and what the usual fix costs.
const PROBLEMS = [
  ['Hidden but accessible models', 'Upstream providers can hide models from lists, but cannot block direct access if the model ID is known.'],
  ['Inaccurate token counting', 'Internal token stats can be jumpy and unreliable for accurate usage tracking.'],
  ['Multi-provider aggregation', 'Coding agents often need to route to multiple providers through a single unified endpoint with custom prefixes.'],
];

const STACK = [
  ['Go', 'One static binary with the web UI embedded. Fast reverse proxy.'],
  ['SQLite', 'Single-file embedded database for lightning-fast logs.'],
  ['Vanilla JS + go:embed', 'The whole dashboard ships inside the binary: no Node build step, no CDN, no external assets.'],
  ['JetBrains Mono Nerd Font', 'Monospace everywhere, so numbers, code and glyphs line up.'],
];

const FEATURES = [
  ['Model Firewall', 'Instantly block or allow specific models with a toggle.'],
  ['Traffic Analytics', 'Log every request, including API key, tokens used, and timestamps.'],
  ['Accounts and 2FA', 'Admin and operator roles, recovery questions, and per-user authenticator enrollment.'],
];

export function mount(root) {
  const el = h('div', { class: 'page' });
  root.append(el);

  const card = (title, sub, ...content) => h('div', { class: 'card' },
    h('div', { class: 'card-head' }, h('div', null, h('h2', null, title), sub ? h('p', { class: 'card-sub' }, sub) : null)), ...content);
  const defs = (items) => h('dl', { class: 'about-list' },
    items.map(([term, text]) => [h('dt', null, term), h('dd', null, text)]).flat());
  const link = (href, ic, text) => h('a', { class: 'btn btn-sm', href, target: '_blank', rel: 'noopener noreferrer', title: text }, icon(ic), text);

  function render() {
    const version = [store.version, store.commit].filter(Boolean).join(' · ') || 'development build';
    el.replaceChildren(
      h('div', { class: 'page-head' }, h('div', null, h('h1', null, `About ${APP}`), h('p', null, TAGLINE))),
      h('div', { class: 'card about-hero' },
        h('img', { src: '/appicon.png', width: 64, height: 64, alt: '' }),
        h('div', null,
          h('h2', null, APP),
          h('p', null, DESCRIPTION),
          h('div', { class: 'about-badges' },
            h('span', { class: 'badge' }, icon('server'), version),
            h('span', { class: 'badge' }, icon('lock'), LICENSE)))),

      h('div', { class: 'grid grid-2e mt' },
        card('Why it exists', 'The problem this was written for', defs(PROBLEMS)),
        card('What it does', 'The short version', defs(FEATURES))),

      h('div', { class: 'grid grid-2 mt' },
        card('Technology', 'What it is built on and why', defs(STACK)),
        card('Author', 'Built and maintained by',
          h('div', { class: 'about-author' },
            h('div', { class: 'about-avatar' }, AUTHOR.name.split(' ').map((w) => w[0]).slice(0, 2).join('')),
            h('div', null,
              h('div', { class: 'strong' }, AUTHOR.name),
              h('div', { class: 'muted' }, `github.com/${AUTHOR.github}`),
              h('div', { class: 'muted' }, AUTHOR.email))),
          h('div', { class: 'about-links' },
            link(`https://github.com/${AUTHOR.github}`, 'github', 'GitHub profile'),
            link(`mailto:${AUTHOR.email}`, 'mail', 'Send an email')))),

      h('div', { class: 'grid grid-2e mt' },
        card('Built with AI assistance', 'Written by a human, with models in the loop',
          h('p', { class: 'about-text' },
            'Parts of this app were designed and written with the help of AI models. ',
            'Every suggestion was reviewed, tested and adjusted by hand before it shipped.')),
        card('Credits', 'Open source this app is built on',
          h('p', { class: 'note' }, icon('sparkles'),
            'Fonts by JetBrains (SIL OFL) and icons from Lucide (ISC). Thanks to both projects.'))));
  }

  render();
  return { refresh: render, destroy() {} };
}
