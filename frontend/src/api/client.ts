const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

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

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(buildApiUrl(path), init);
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
  return res.json() as Promise<T>;
}

export function apiGet<T>(
  path: string,
  params?: Record<string, string | number | undefined>,
): Promise<T> {
  return request<T>(buildApiUrl(path, params));
}

export function apiPost<T>(path: string): Promise<T> {
  return request<T>(buildApiUrl(path), { method: "POST" });
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
