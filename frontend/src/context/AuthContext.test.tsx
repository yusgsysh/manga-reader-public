// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./AuthContext";
import { useAuth } from "./auth";
import {
  fetchChecked,
  getAccessToken,
  setAccessToken,
} from "../api/client";

interface RecordedCall {
  url: string;
  init?: RequestInit;
}

function mockFetch(
  handler: (url: string, init: RequestInit | undefined, call: number) => Response,
): RecordedCall[] {
  const calls: RecordedCall[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      calls.push({ url, init });
      return handler(url, init, calls.length);
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

const meResponse = {
  id: 1,
  user_id: 1,
  username: "alice",
  role: "admin",
};

function Probe() {
  const { user, isLoading, error, login, logout, isAdmin, isAuthenticated } =
    useAuth();
  if (isLoading) return <div>loading</div>;
  return (
    <div>
      <span data-testid="username">{user?.username ?? "anon"}</span>
      <span data-testid="admin">{String(isAdmin)}</span>
      <span data-testid="authed">{String(isAuthenticated)}</span>
      {error && <span data-testid="error">{error.message}</span>}
      <button
        onClick={() => {
          void login("alice", "secret-pass").catch(() => {});
        }}
      >
        login
      </button>
      <button
        onClick={() => {
          void logout();
        }}
      >
        logout
      </button>
    </div>
  );
}

function renderAuth() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Probe />
      </AuthProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

afterEach(() => {
  vi.unstubAllGlobals();
  setAccessToken(null);
});

describe("AuthProvider", () => {
  it("restores the session from the HttpOnly cookie on refresh", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      if (url.endsWith("/api/auth/me")) return json(meResponse);
      return json({ error: "not found" }, 404);
    });

    renderAuth();

    expect(await screen.findByText("alice")).toBeTruthy();
    expect(screen.getByTestId("admin").textContent).toBe("true");
    expect(screen.getByTestId("authed").textContent).toBe("true");
  });

  it("exposes a terminal error when the session cannot be loaded", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) return json({ error: "boom" }, 500);
      return json({}, 500);
    });

    renderAuth();

    expect(await screen.findByTestId("error")).toBeTruthy();
    expect(screen.getByTestId("username").textContent).toBe("anon");
  });

  it("updates the session after login and sends the Bearer token", async () => {
    let meCalls = 0;
    mockFetch((url, init) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      if (url.endsWith("/api/auth/login")) {
        return json({
          access_token: "signed.jwt.token",
          token_type: "Bearer",
          expires_in: 3600,
        });
      }
      if (url.endsWith("/api/auth/me")) {
        meCalls++;
        if (meCalls === 1) return json({ error: "unauthorized" }, 401);
        const headers = new Headers(init?.headers);
        if (headers.get("Authorization") !== "Bearer signed.jwt.token") {
          return json({ error: "missing token" }, 401);
        }
        return json(meResponse);
      }
      return json({ error: "not found" }, 404);
    });

    renderAuth();
    expect(await screen.findByText("anon")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "login" }));

    expect(await screen.findByText("alice")).toBeTruthy();
    expect(screen.getByTestId("authed").textContent).toBe("true");
  });

  it("clears user data, cached queries, and the in-memory token on logout", async () => {
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      if (url.endsWith("/api/auth/me")) return json(meResponse);
      if (url.endsWith("/api/auth/logout")) return json({ success: true });
      return json({ error: "not found" }, 404);
    });

    const queryClient = renderAuth();
    expect(await screen.findByText("alice")).toBeTruthy();
    queryClient.setQueryData(["bookshelf", 1], { total: 1 });
    setAccessToken("signed.jwt.token");

    await userEvent.click(screen.getByRole("button", { name: "logout" }));

    await waitFor(() => {
      expect(screen.getByTestId("username").textContent).toBe("anon");
    });
    expect(queryClient.getQueryData(["bookshelf", 1])).toBeUndefined();
    expect(getAccessToken()).toBeNull();
    const logoutCall = calls.find((call) => call.url.endsWith("/api/auth/logout"));
    expect(new Headers(logoutCall?.init?.headers).get("Authorization")).toBe(
      "Bearer signed.jwt.token",
    );
  });

  it("resets the session and caches on a 401 from any API call", async () => {
    mockFetch((url) => {
      if (url.endsWith("/api/auth/status")) {
        return json({ enabled: true, initialized: true });
      }
      if (url.endsWith("/api/auth/me")) return json(meResponse);
      if (url.endsWith("/api/bookshelf")) return json({ error: "unauthorized" }, 401);
      return json({ error: "not found" }, 404);
    });

    const queryClient = renderAuth();
    expect(await screen.findByText("alice")).toBeTruthy();
    queryClient.setQueryData(["bookshelf", 1], { total: 1 });

    await act(async () => {
      await expect(fetchChecked("http://localhost/api/bookshelf")).rejects.toThrow();
    });

    await waitFor(() => {
      expect(screen.getByTestId("username").textContent).toBe("anon");
    });
    expect(queryClient.getQueryData(["bookshelf", 1])).toBeUndefined();
    expect(getAccessToken()).toBeNull();
  });
});
