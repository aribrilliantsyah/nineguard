// REST client for the server API. Authentication is the HttpOnly session
// cookie set by /login, so requests carry no token of their own.

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

export function qs(params = {}) {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') u.set(k, v);
  }
  const s = u.toString();
  return s ? '?' + s : '';
}

// Session gone: go to the sign-in page and come back to the same view after.
export function redirectToLogin() {
  location.href = '/login?next=' + encodeURIComponent(location.pathname + location.search) + location.hash;
}

async function request(method, path, { params, body } = {}) {
  let resp;
  try {
    resp = await fetch('/api/v1' + path + qs(params), {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError(0, 'Cannot reach the NineGuard server');
  }
  if (resp.status === 401 && !path.startsWith('/auth/')) {
    redirectToLogin();
    throw new ApiError(401, 'Sign in required');
  }
  const text = await resp.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* non-JSON body */ }
  if (!resp.ok) throw new ApiError(resp.status, (data && data.error) || `Request failed (HTTP ${resp.status})`);
  return data;
}

export const api = {
  get: (path, params) => request('GET', path, { params }),
  post: (path, body = {}) => request('POST', path, { body }),
  put: (path, body = {}) => request('PUT', path, { body }),
  patch: (path, body = {}) => request('PATCH', path, { body }),
  del: (path, params) => request('DELETE', path, { params }),

  // Downloads a server-generated file (export).
  async download(path, params) {
    const resp = await fetch('/api/v1' + path + qs(params));
    if (resp.status === 401) return redirectToLogin();
    if (!resp.ok) {
      let msg = `Export failed (HTTP ${resp.status})`;
      try { msg = (await resp.json()).error || msg; } catch { /* keep default */ }
      throw new ApiError(resp.status, msg);
    }
    const name = /filename="([^"]+)"/.exec(resp.headers.get('Content-Disposition') || '')?.[1] || 'nineguard-export';
    const url = URL.createObjectURL(await resp.blob());
    const a = Object.assign(document.createElement('a'), { href: url, download: name });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  },
};
