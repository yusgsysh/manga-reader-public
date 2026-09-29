const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

const DEFAULT_RETRY_COUNT = 2;
const DEFAULT_RETRY_DELAY_MS = 500;
const DEFAULT_TIMEOUT_MS = 30_000;

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

function isRetryableStatus(status: number): boolean {
  return status === 408 || status === 429 || status >= 500;
}

function sleep(ms: number, signal?: AbortSignal | null): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function fetchWithTimeout(
  path: string,
  init?: RequestInit,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(path, { ...init, signal: controller.signal });
  } finally {
    clearTimeout(timer);
  }
}

const inflightRequests = new Map<string, Promise<unknown>>();

async function fetchChecked(
  path: string,
  init?: RequestInit,
  retryCount = DEFAULT_RETRY_COUNT,
): Promise<Response> {
  const cacheKey = `${init?.method ?? "GET"}:${path}`;

  if (retryCount <= 0 && inflightRequests.has(cacheKey)) {
    return inflightRequests.get(cacheKey) as Promise<Response>;
  }

  const doFetch = async (): Promise<Response> => {
    let lastError: unknown;
    for (let attempt = 0; attempt <= retryCount; attempt++) {
      try {
        const res = await fetchWithTimeout(path, init);
        if (!res.ok) {
          let message = `Request failed (${res.status})`;
          try {
            const body = await res.json();
            if (body.error) message = body.error;
          } catch {
            // ignore parse error
          }
          const error = new ApiRequestError(res.status, message);
          if (!isRetryableStatus(res.status) || attempt === retryCount) {
            throw error;
          }
          lastError = error;
          const delay = DEFAULT_RETRY_DELAY_MS * 2 ** attempt + Math.random() * 100;
          await sleep(delay, init?.signal);
          continue;
        }
        return res;
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") throw err;
        if (err instanceof ApiRequestError && !isRetryableStatus(err.status)) throw err;
        lastError = err;
        if (attempt < retryCount) {
          const delay = DEFAULT_RETRY_DELAY_MS * 2 ** attempt + Math.random() * 100;
          await sleep(delay, init?.signal);
        }
      }
    }
    throw lastError;
  };

  if (retryCount > 0) {
    const promise = doFetch().finally(() => {
      inflightRequests.delete(cacheKey);
    });
    inflightRequests.set(cacheKey, promise);
    return promise;
  }
  return doFetch();
}

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetchChecked(path, init, 0);
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

export async function apiBlob(
  path: string,
  params?: Record<string, string | number | undefined>,
  init?: RequestInit,
): Promise<Blob> {
  const res = await fetchChecked(buildApiUrl(path, params), init);
  return res.blob();
}

export async function apiHead(
  path: string,
  params?: Record<string, string | number | undefined>,
): Promise<Response> {
  return fetchChecked(buildApiUrl(path, params), { method: "HEAD" });
}
