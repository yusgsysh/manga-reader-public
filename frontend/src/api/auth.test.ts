import { afterEach, describe, expect, it, vi } from "vitest";
import {
  fetchAuthSession,
  login,
  logout,
} from "./auth";
import {
  apiGet,
  fetchChecked,
  setAccessToken,
  setUnauthorizedHandler,
} from "./client";

interface RecordedCall {
  url: string;
  init?: RequestInit;
}

function mockFetch(
  handler: (url: string, init?: RequestInit) => Response,
): RecordedCall[] {
  const calls: RecordedCall[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      calls.push({ url, init });
      return handler(url, init);
    }),
  );
  return calls;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  setAccessToken(null);
  setUnauthorizedHandler(null);
});

describe("auth session", () => {
  it("restores a logged-in user from the HttpOnly cookie", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      if (url.endsWith("/api/auth/me")) {
        return json({
          id: 1,
          user_id: 1,
          username: "admin",
          role: "admin",
        });
      }
      return json({ error: "not found" }, 404);
    });

    await expect(fetchAuthSession()).resolves.toEqual({
      enabled: true,
      initialized: true,
      user: {
        id: 1,
        user_id: 1,
        username: "admin",
        role: "admin",
      },
    });
  });

  it("treats a 401 from /me as an expired session", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      return json({ error: "unauthorized" }, 401);
    });

    await expect(fetchAuthSession()).resolves.toEqual({
      enabled: true,
      initialized: true,
      user: null,
    });
  });

  it("skips the login wall when auth is disabled", async () => {
    mockFetch(() => json({ enabled: false, initialized: false }));

    await expect(fetchAuthSession()).resolves.toEqual({
      enabled: false,
      initialized: false,
      user: null,
    });
  });
});

describe("authenticated requests", () => {
  it("logs in and attaches the Bearer token to later calls", async () => {
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/login")) {
        return json({
          access_token: "signed.jwt.token",
          token_type: "Bearer",
          expires_in: 3600,
        });
      }
      if (url.endsWith("/api/auth/me")) {
        return json({
          id: 1,
          user_id: 1,
          username: "admin",
          role: "admin",
        });
      }
      return json({ ok: true });
    });

    await expect(login("admin", "password123")).resolves.toEqual({
      user: {
        id: 1,
        user_id: 1,
        username: "admin",
        role: "admin",
      },
    });

    const loginCall = calls.find((call) => call.url.endsWith("/api/auth/login"));
    expect(JSON.parse(String(loginCall?.init?.body))).toMatchObject({
      username: "admin",
    });

    await apiGet("/api/bookshelf");
    const bookshelf = calls.find((call) => call.url.endsWith("/api/bookshelf"));
    const headers = new Headers(bookshelf?.init?.headers);
    expect(headers.get("Authorization")).toBe("Bearer signed.jwt.token");
  });

  it("clears the in-memory token on logout", async () => {
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/login")) {
        return json({
          access_token: "signed.jwt.token",
          token_type: "Bearer",
          expires_in: 3600,
        });
      }
      if (url.endsWith("/api/auth/me")) {
        return json({
          id: 1,
          user_id: 1,
          username: "admin",
          role: "admin",
        });
      }
      return json({ success: true });
    });

    await login("admin", "password123");
    await logout();
    await apiGet("/api/bookshelf");

    const bookshelf = calls.find((call) => call.url.endsWith("/api/bookshelf"));
    expect(new Headers(bookshelf?.init?.headers).get("Authorization")).toBeNull();
  });

  it("coalesces concurrent 401s into one auth reset", async () => {
    mockFetch(() => json({ error: "unauthorized" }, 401));
    const onUnauthorized = vi.fn();
    setUnauthorizedHandler(onUnauthorized);

    const results = await Promise.allSettled([
      fetchChecked("http://localhost/api/bookshelf"),
      fetchChecked("http://localhost/api/recently-read"),
      fetchChecked("http://localhost/api/prefill"),
    ]);
    await Promise.resolve();

    expect(results.every((result) => result.status === "rejected")).toBe(true);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });
});
