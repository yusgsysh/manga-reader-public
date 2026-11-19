// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./AuthContext";
import { useAuth } from "./auth";
import { fetchChecked, setAccessToken } from "../api/client";

const kinde = vi.hoisted(() => ({
  current: {
    isLoading: false,
    isAuthenticated: false,
    getAccessToken: vi.fn(async () => "signed.jwt.token"),
    refreshToken: vi.fn(async () => ({ success: true })),
    login: vi.fn(async () => undefined),
    register: vi.fn(async () => undefined),
    logout: vi.fn(async () => undefined),
  },
}));

vi.mock("@kinde-oss/kinde-auth-react", () => ({
  useKindeAuth: () => kinde.current,
}));

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

function Probe() {
  const {
    user,
    isLoading,
    error,
    login,
    logout,
    isAdmin,
    isAuthenticated,
  } = useAuth();
  if (isLoading) return <div>loading</div>;
  return (
    <div>
      <span data-testid="username">{user?.name ?? "anon"}</span>
      <span data-testid="admin">{String(isAdmin)}</span>
      <span data-testid="authed">{String(isAuthenticated)}</span>
      {error && <span data-testid="error">{error.message}</span>}
      <button onClick={() => void login("/bookshelf").catch(() => {})}>
        login
      </button>
      <button onClick={() => void logout()}>logout</button>
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

function resetKinde() {
  kinde.current = {
    isLoading: false,
    isAuthenticated: false,
    getAccessToken: vi.fn(async () => "signed.jwt.token"),
    refreshToken: vi.fn(async () => ({ success: true })),
    login: vi.fn(async () => undefined),
    register: vi.fn(async () => undefined),
    logout: vi.fn(async () => undefined),
  };
  sessionStorage.clear();
}

afterEach(() => {
  vi.unstubAllGlobals();
  setAccessToken(null);
  resetKinde();
});

describe("AuthProvider", () => {
  it("shows the loading state while the SDK restores a session", () => {
    resetKinde();
    kinde.current.isLoading = true;
    mockFetch(() => json({}, 500));
    renderAuth();
    expect(screen.getByText("loading")).toBeTruthy();
  });

  it("probes /api/auth/me once and stays signed out on 401", async () => {
    resetKinde();
    const calls = mockFetch(() => json({ error: "unauthorized" }, 401));
    renderAuth();

    expect(await screen.findByText("anon")).toBeTruthy();
    expect(screen.getByTestId("authed").textContent).toBe("false");
    expect(calls.filter((call) => call.url.endsWith("/api/auth/me"))).toHaveLength(1);
  });

  it("restores the local identity from /api/auth/me after a Kinde refresh", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    mockFetch((url) => {
      if (url.endsWith("/api/auth/me")) return json(me);
      return json({ error: "not found" }, 404);
    });

    renderAuth();

    expect(await screen.findByText("Alice")).toBeTruthy();
    expect(screen.getByTestId("admin").textContent).toBe("true");
    expect(screen.getByTestId("authed").textContent).toBe("true");
  });

  it("treats a 401 from /me as signed out, not as an error", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    mockFetch(() => json({ error: "unauthorized" }, 401));
    renderAuth();

    expect(await screen.findByText("anon")).toBeTruthy();
    expect(screen.queryByTestId("error")).toBeNull();
  });

  it("surfaces a disabled local account instead of redirecting", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    mockFetch(() => json({ error: "forbidden" }, 403));
    renderAuth();

    expect(await screen.findByTestId("error")).toBeTruthy();
    expect(screen.getByTestId("error").textContent).toContain("账号已被禁用");
  });

  it("remembers the destination and starts the Kinde login redirect", async () => {
    resetKinde();
    mockFetch(() => json({ error: "unauthorized" }, 401));
    renderAuth();
    await screen.findByText("anon");

    await userEvent.click(screen.getByRole("button", { name: "login" }));

    expect(kinde.current.login).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem("manga-reader:return-to")).toBe("/bookshelf");
  });

  it("revokes local state and calls the SDK logout", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    mockFetch((url) => {
      if (url.endsWith("/api/auth/me")) return json(me);
      if (url.endsWith("/api/auth/logout")) return json({ success: true });
      return json({ error: "not found" }, 404);
    });

    const queryClient = renderAuth();
    expect(await screen.findByText("Alice")).toBeTruthy();
    queryClient.setQueryData(["bookshelf", 1], { total: 1 });

    await userEvent.click(screen.getByRole("button", { name: "logout" }));

    await waitFor(() => {
      expect(screen.getByTestId("username").textContent).toBe("anon");
    });
    expect(queryClient.getQueryData(["bookshelf", 1])).toBeUndefined();
    expect(kinde.current.logout).toHaveBeenCalledTimes(1);
  });

  it("attaches the SDK access token to API requests", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    const calls = mockFetch((url) => {
      if (url.endsWith("/api/auth/me")) return json(me);
      return json({ ok: true });
    });
    renderAuth();
    await screen.findByText("Alice");

    await act(async () => {
      await fetchChecked("/api/bookshelf");
    });

    const call = calls.find((entry) => entry.url.endsWith("/api/bookshelf"));
    expect(new Headers(call?.init?.headers).get("Authorization")).toBe(
      "Bearer signed.jwt.token",
    );
  });

  it("clears the identity and caches on a 401 from a business API", async () => {
    resetKinde();
    kinde.current.isAuthenticated = true;
    mockFetch((url) => {
      if (url.endsWith("/api/auth/me")) return json(me);
      return json({ error: "unauthorized" }, 401);
    });
    const queryClient = renderAuth();
    await screen.findByText("Alice");
    queryClient.setQueryData(["bookshelf", 1], { total: 1 });

    await act(async () => {
      await expect(fetchChecked("/api/bookshelf")).rejects.toThrow();
    });

    await waitFor(() => {
      expect(screen.getByTestId("username").textContent).toBe("anon");
    });
    expect(queryClient.getQueryData(["bookshelf", 1])).toBeUndefined();
  });
});
