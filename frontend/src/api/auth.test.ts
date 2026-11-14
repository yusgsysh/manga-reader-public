import { afterEach, describe, expect, it, vi } from "vitest";
import {
  fetchAuthSession,
  login,
  register,
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

const adminUser = {
  id: "00000000-0000-0000-0000-000000000001",
  user_id: "00000000-0000-0000-0000-000000000001",
  username: "admin",
  role: "admin",
};

afterEach(() => {
  vi.unstubAllGlobals();
  setAccessToken(null);
  setUnauthorizedHandler(null);
});

describe("auth session", () => {
  it("restores a logged-in user from a single status probe", async () => {
    const me = {
      id: "00000000-0000-0000-0000-000000000001",
      user_id: "00000000-0000-0000-0000-000000000001",
      username: "admin",
      role: "admin",
    };
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ initialized: true, user: me });
      }
      return json({ error: "not found" }, 404);
    });

    await expect(fetchAuthSession()).resolves.toEqual({
      initialized: true,
      user: me,
    });
    // The refresh path must not pay for a second /me round-trip.
    expect(calls.map((call) => call.url)).toEqual([
      expect.stringMatching(/\/api\/auth\/status$/),
    ]);
  });

  it("treats a status probe without a user as signed out", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ initialized: true, user: null });
      }
      return json({ error: "unauthorized" }, 401);
    });

    await expect(fetchAuthSession()).resolves.toEqual({
      initialized: true,
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
          user: adminUser,
        });
      }
      return json({ ok: true });
    });

    await expect(login("admin", "password123")).resolves.toEqual({
      user: adminUser,
    });

    // The identity comes from the login response itself.
    expect(calls.some((call) => call.url.endsWith("/api/auth/me"))).toBe(false);

    const loginCall = calls.find((call) => call.url.endsWith("/api/auth/login"));
    expect(JSON.parse(String(loginCall?.init?.body))).toMatchObject({
      username: "admin",
    });

    await apiGet("/api/bookshelf");
    const bookshelf = calls.find((call) => call.url.endsWith("/api/bookshelf"));
    const headers = new Headers(bookshelf?.init?.headers);
    expect(headers.get("Authorization")).toBe("Bearer signed.jwt.token");
  });

  it("registers the first administrator and attaches the Bearer token", async () => {
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/register")) {
        return json({
          access_token: "signed.jwt.token",
          token_type: "Bearer",
          expires_in: 3600,
          user: adminUser,
        });
      }
      return json({ ok: true });
    });

    await expect(register("admin", "password123")).resolves.toEqual({
      user: adminUser,
    });

    expect(calls.some((call) => call.url.endsWith("/api/auth/me"))).toBe(false);

    const registerCall = calls.find((call) =>
      call.url.endsWith("/api/auth/register"),
    );
    expect(JSON.parse(String(registerCall?.init?.body))).toMatchObject({
      username: "admin",
      password: "password123",
    });

    await apiGet("/api/bookshelf");
    const bookshelf = calls.find((call) => call.url.endsWith("/api/bookshelf"));
    expect(new Headers(bookshelf?.init?.headers).get("Authorization")).toBe(
      "Bearer signed.jwt.token",
    );
  });

  it("clears the in-memory token on logout", async () => {
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/login")) {
        return json({
          access_token: "signed.jwt.token",
          token_type: "Bearer",
          expires_in: 3600,
          user: adminUser,
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
