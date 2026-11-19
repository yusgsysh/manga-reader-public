import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchAuthUser, logout } from "./auth";
import { ApiRequestError, setAccessToken, setUnauthorizedHandler } from "./client";

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

const me = {
  id: "00000000-0000-7000-8000-000000000001",
  user_id: "kp_alice",
  username: "kinde_1234567890abcdef",
  email: "alice@example.com",
  name: "Alice",
  avatar_url: null,
  role: "admin",
  status: "active",
};

afterEach(() => {
  vi.unstubAllGlobals();
  setAccessToken(null);
  setUnauthorizedHandler(null);
});

describe("auth API", () => {
  it("loads the local account for the verified access token", async () => {
    const calls = mockFetch(() => json(me));
    setAccessToken("signed.jwt.token");

    await expect(fetchAuthUser()).resolves.toEqual(me);
    const call = calls.find((entry) => entry.url.endsWith("/api/auth/me"));
    expect(new Headers(call?.init?.headers).get("Authorization")).toBe(
      "Bearer signed.jwt.token",
    );
  });

  it("does not hide 403: a valid identity may be locally disabled", async () => {
    mockFetch(() => json({ error: "forbidden" }, 403));
    await expect(fetchAuthUser()).rejects.toMatchObject({
      status: 403,
    });
  });

  it("treats 401 as an expired session and notifies the auth layer", async () => {
    const unauthorized = vi.fn();
    setUnauthorizedHandler(unauthorized);
    mockFetch(() => json({ error: "unauthorized" }, 401));

    await expect(fetchAuthUser()).rejects.toBeInstanceOf(ApiRequestError);
    await Promise.resolve();
    expect(unauthorized).toHaveBeenCalledTimes(1);
  });

  it("posts logout with the bearer token", async () => {
    const calls = mockFetch(() => json({ success: true }));
    setAccessToken("signed.jwt.token");

    await logout();

    const call = calls.find((entry) => entry.url.endsWith("/api/auth/logout"));
    expect(call?.init?.method).toBe("POST");
    expect(new Headers(call?.init?.headers).get("Authorization")).toBe(
      "Bearer signed.jwt.token",
    );
  });
});
