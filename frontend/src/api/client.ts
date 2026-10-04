declare global {
  interface Window {
    /**
     * Set by the desktop shell: it owns the loopback port Gin bound to, which
     * is only known once the app is running.
     */
    __MANGA_READER_CONFIG__?: { apiBaseUrl?: string };
  }
}

/**
 * Where the REST API lives, resolved once at load time:
 *
 *  1. `window.__MANGA_READER_CONFIG__` — injected by the Wails desktop shell,
 *     which starts Gin on a random 127.0.0.1 port.
 *  2. `VITE_API_BASE_URL` — baked in at build time. An empty string means
 *     "same origin", which is what the web/Docker build wants behind nginx.
 *  3. `http://localhost:8080` — the local dev backend.
 */
function resolveApiBaseUrl(): string {
  if (typeof window !== "undefined") {
    const injected = window.__MANGA_READER_CONFIG__?.apiBaseUrl;
    if (injected) return injected;
  }
  return import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";
}

const API_BASE_URL = resolveApiBaseUrl();

/**
 * Stand-in absolute origin used only to reuse URL's query-string handling when
 * the API is same-origin. `window.location.origin` cannot be used for this:
 * the desktop shell serves pages from a `wails://` origin, where it is the
 * string "null" rather than a real URL.
 */
const SAME_ORIGIN_BASE = "http://manga-reader.same-origin.invalid";

export class ApiRequestError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiRequestError";
    this.status = status;
  }
}

export function buildApiUrl(
  path: string,
  params?: Record<string, string | number | undefined>,
): string {
  const url = new URL(path, API_BASE_URL || SAME_ORIGIN_BASE);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") {
        url.searchParams.set(key, String(value));
      }
    }
  }
  // Same origin: hand back a relative path so the request stays on whatever
  // origin served the page (browser, nginx, or the desktop shell's proxy).
  if (!API_BASE_URL) {
    return url.pathname + url.search + url.hash;
  }
  return url.toString();
}

export async function fetchChecked(path: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(path, init);
  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = await res.json();
      if (body.error) message = body.error;
    } catch {
      // ignore parse error
    }
    throw new ApiRequestError(res.status, message);
  }
  return res;
}

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetchChecked(path, init);
  return res.json() as Promise<T>;
}

export function apiGet<T>(
  path: string,
  params?: Record<string, string | number | undefined>,
): Promise<T> {
  return request<T>(buildApiUrl(path, params));
}

export function apiPost<T>(
  path: string,
  params?: Record<string, string | number | undefined>,
): Promise<T> {
  return request<T>(buildApiUrl(path, params), { method: "POST" });
}

export function apiPostJson<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(buildApiUrl(path), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
}

export function apiPut<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(buildApiUrl(path), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
}

export function apiDelete<T>(path: string): Promise<T> {
  return request<T>(buildApiUrl(path), { method: "DELETE" });
}

export async function apiHead(
  path: string,
  params?: Record<string, string | number | undefined>,
): Promise<Response> {
  return fetchChecked(buildApiUrl(path, params), { method: "HEAD" });
}
