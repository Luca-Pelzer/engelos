// The dashboard is served by the engelOS daemon on the same origin, so API
// calls and OAuth redirects use same-origin relative URLs by default. A blank
// base resolves "/api/v1/..." against whatever host served the page (for
// example https://bot.engels.wtf), which is exactly what we want in production
// and behind the reverse proxy. Set VITE_API_BASE to target a different host
// during local development where the SPA and daemon run on separate ports.
export const API_BASE =
  (typeof import.meta !== 'undefined' && (import.meta as ImportMeta & { env?: Record<string, string> }).env?.VITE_API_BASE) ||
  '';

export type ApiError = {
  status: number;
  message: string;
  details?: unknown;
};

export class ApiException extends Error {
  status: number;
  details?: unknown;
  constructor(err: ApiError) {
    super(err.message);
    this.name = 'ApiException';
    this.status = err.status;
    this.details = err.details;
  }
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

let authToken: string | null = null;

export function setAuthToken(token: string | null): void {
  authToken = token;
}

export function getAuthToken(): string | null {
  return authToken;
}

export async function request<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  const url = path.startsWith('http') ? path : `${API_BASE}${path}`;
  const headers: Record<string, string> = {
    Accept: 'application/json',
    ...opts.headers,
  };
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (authToken) {
    headers.Authorization = `Bearer ${authToken}`;
  }

  let res: Response;
  try {
    res = await fetch(url, {
      method: opts.method ?? 'GET',
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal,
      credentials: 'include',
    });
  } catch (err) {
    throw new ApiException({
      status: 0,
      message: err instanceof Error ? err.message : 'Network unreachable',
    });
  }

  if (!res.ok) {
    let details: unknown = undefined;
    let message = res.statusText || 'Request failed';
    try {
      const data = await res.json();
      details = data;
      if (typeof data?.message === 'string') message = data.message;
      else if (typeof data?.error === 'string') message = data.error;
    } catch {
      /* non-JSON error body, ignore */
    }
    throw new ApiException({ status: res.status, message, details });
  }

  if (res.status === 204) return undefined as T;
  const ct = res.headers.get('content-type') ?? '';
  if (ct.includes('application/json')) {
    return (await res.json()) as T;
  }
  return (await res.text()) as unknown as T;
}

export const api = {
  get:    <T = unknown>(path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) => request<T>(path, { ...opts, method: 'GET' }),
  post:   <T = unknown>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) => request<T>(path, { ...opts, method: 'POST', body }),
  put:    <T = unknown>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) => request<T>(path, { ...opts, method: 'PUT', body }),
  patch:  <T = unknown>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) => request<T>(path, { ...opts, method: 'PATCH', body }),
  delete: <T = unknown>(path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) => request<T>(path, { ...opts, method: 'DELETE' }),
};

export type LoginRequest = { email: string; password: string };
export type LoginResponse = { token: string; user: { id: string; email: string; role: string } };
export type CurrentUser = { id: string; email: string; username: string; role: string };

export const auth = {
  login: (body: LoginRequest) => api.post<LoginResponse>('/api/v1/auth/login', body),
  logout: () => api.post('/api/v1/auth/logout'),
  me: () => api.get<CurrentUser>('/api/v1/users/me'),
};
