// Production builds default to same-origin /api/* (the Docker build also sets
// VITE_API_BASE_URL="" explicitly). The bare `bun dev` server has no proxy,
// so development keeps the local backend as its default.
const API_BASE_URL =
  import.meta.env.VITE_API_BASE_URL ??
  (import.meta.env.DEV ? "http://localhost:8080" : "");

// Access tokens are owned by the Kinde SDK (refresh, storage and rotation).
// The client asks this provider for a token on every request instead of
// keeping a second copy in application state. `setAccessToken` remains for
// tests and non-Kinde callers that explicitly manage a single token.
let accessToken: string | null = null;
let accessTokenProvider: (() => Promise<string | null | undefined>) | null =
  null;

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

export function getAccessToken(): string | null {
  return accessToken;
}

/** Installs the single source of access tokens (normally Kinde). */
export function setAccessTokenProvider(
  provider: (() => Promise<string | null | undefined>) | null,
): void {
  accessTokenProvider = provider;
}

async function currentAccessToken(): Promise<string | null> {
  if (accessToken) return accessToken;
  if (!accessTokenProvider) return null;
  try {
    return (await accessTokenProvider()) ?? null;
  } catch {
    // A refresh failure must not turn every API call into an exception; the
    // request proceeds without a token and the server's 401 drives the UI.
    return null;
  }
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
  // Same-origin requests are authenticated only by the explicit Bearer header;
  // the backend's media cookie is reserved for <img>/SSE/download requests.
  // In the Vite dev setup the API is cross-origin (5173 -> 8080) and needs an
  // explicit credentials mode.
  const crossOrigin =
    typeof window !== "undefined" &&
    new URL(path, window.location.origin).origin !== window.location.origin;
  const token = await currentAccessToken();

  let nextInit = init;
  if (token || crossOrigin) {
    const headers = new Headers(init?.headers);
    if (token && !headers.has("Authorization")) {
      headers.set("Authorization", `Bearer ${token}`);
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
    // 401 means the Kinde session or its refresh is no longer usable. 403 is a
    // valid identity without application access (e.g. a locally disabled
    // user) and must not clear the session.
    if (res.status === 401) {
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
