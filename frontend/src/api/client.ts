// Production builds default to same-origin /api/* (the Docker build also sets
// VITE_API_BASE_URL="" explicitly). The bare `bun dev` server has no proxy,
// so development keeps the local backend as its default.
const API_BASE_URL =
  import.meta.env.VITE_API_BASE_URL ??
  (import.meta.env.DEV ? "http://localhost:8080" : "");

// The JWT returned by /api/auth/login is primarily stored by the browser in an
// HttpOnly cookie (set server-side). This in-memory copy lets same-tab calls
// send the standard Authorization header without exposing the token to
// localStorage/XSS. A refresh restores the session through the cookie.
let accessToken: string | null = null;

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

export function getAccessToken(): string | null {
  return accessToken;
}

let unauthorizedHandler: (() => void) | null = null;
let unauthorizedScheduled = false;

/** Registers the single auth-state reset used by concurrent 401 responses. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

// Coalesce a burst of 401s (several queries failing together) into one
// redirect. Without this every request would race to navigate to /login.
function notifyUnauthorized(): void {
  accessToken = null;
  if (unauthorizedScheduled) return;
  unauthorizedScheduled = true;
  queueMicrotask(() => {
    unauthorizedScheduled = false;
    unauthorizedHandler?.();
  });
}

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
  const url = API_BASE_URL
    ? new URL(path, API_BASE_URL)
    : new URL(path, window.location.origin);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") {
        url.searchParams.set(key, String(value));
      }
    }
  }
  return url.toString();
}

export async function fetchChecked(path: string, init?: RequestInit): Promise<Response> {
  // Same-origin requests send the HttpOnly cookie by default. In the Vite
  // dev setup the API is cross-origin (5173 -> 8080) and needs an explicit
  // credentials mode. Keep the original init object otherwise so existing
  // callers/tests see an unchanged fetch contract.
  const crossOrigin =
    typeof window !== "undefined" &&
    new URL(path, window.location.origin).origin !== window.location.origin;

  let nextInit = init;
  if (accessToken || crossOrigin) {
    const headers = new Headers(init?.headers);
    if (accessToken && !headers.has("Authorization")) {
      headers.set("Authorization", `Bearer ${accessToken}`);
    }
    nextInit = {
      ...init,
      headers,
      credentials: init?.credentials ?? "include",
    };
  }

  const res = await fetch(path, nextInit);
  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = await res.json();
      if (body.error) message = body.error;
    } catch {
      // ignore parse error
    }
    // A failed login is an expected credential error, not a session expiry.
    if (res.status === 401 && !path.includes("/api/auth/login")) {
      notifyUnauthorized();
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

export function apiPatchJson<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(buildApiUrl(path), {
    method: "PATCH",
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
