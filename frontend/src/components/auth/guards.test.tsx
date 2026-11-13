// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
} from "react-router";
import { ThemeProvider } from "../../lib/theme";
import {
  AuthContext,
  type AuthContextValue,
} from "../../context/auth";
import { ApiRequestError } from "../../api/client";
import { RequireAuth } from "./RequireAuth";
import { LoginPage } from "../../pages/LoginPage";

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    session: null,
    user: null,
    role: null,
    isAuthenticated: false,
    isAdmin: false,
    isLoading: false,
    error: null,
    isLoggingIn: false,
    isLoggingOut: false,
    isRegistering: false,
    login: vi.fn(),
    register: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    ...overrides,
  };
}

function LoginProbe() {
  const location = useLocation();
  return (
    <div>
      <span>login-page</span>
      <span data-testid="from">
        {(location.state as { from?: string } | null)?.from ?? ""}
      </span>
    </div>
  );
}

function SetupProbe() {
  return <span>setup-page</span>;
}

function renderGuard(value: AuthContextValue, initialPath: string) {
  return render(
    <AuthContext.Provider value={value}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/private"
            element={
              <RequireAuth>
                <div>private-content</div>
              </RequireAuth>
            }
          />
          <Route path="/" element={<div>home-marker</div>} />
          <Route path="/login" element={<LoginProbe />} />
          <Route path="/setup" element={<SetupProbe />} />
        </Routes>
      </MemoryRouter>
    </AuthContext.Provider>,
  );
}

describe("RequireAuth", () => {
  it("redirects an unauthenticated visitor to /login and preserves the destination", () => {
    renderGuard(authValue(), "/private?tab=all");
    expect(screen.getByText("login-page")).toBeTruthy();
    expect(screen.getByTestId("from").textContent).toBe("/private?tab=all");
    expect(screen.queryByText("private-content")).toBeNull();
  });

  it("renders the page when authenticated", () => {
    renderGuard(
      authValue({
        isAuthenticated: true,
        user: { id: 1, user_id: 1, username: "alice", role: "user" },
      }),
      "/private",
    );
    expect(screen.getByText("private-content")).toBeTruthy();
  });

  it("shows a retryable error instead of redirecting on a session error", () => {
    const refresh = vi.fn();
    renderGuard(
      authValue({ error: new Error("offline"), refresh }),
      "/private",
    );
    expect(screen.getByText(/无法连接服务器/)).toBeTruthy();
    screen.getByRole("button").click();
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("redirects to the setup page when the instance is uninitialized", () => {
    renderGuard(
      authValue({ session: { initialized: false, user: null } }),
      "/private",
    );
    expect(screen.getByText("setup-page")).toBeTruthy();
    expect(screen.queryByText("private-content")).toBeNull();
  });
});

function renderLogin(
  value: AuthContextValue,
  initialEntry: { pathname: string; state?: unknown } = { pathname: "/login" },
) {
  return render(
    <AuthContext.Provider value={value}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home-marker</div>} />
            <Route path="/bookshelf" element={<div>bookshelf-marker</div>} />
            <Route path="/setup" element={<SetupProbe />} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>
    </AuthContext.Provider>,
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    localStorage.clear();
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });
  });

  it("rejects an empty username or password before calling the API", async () => {
    const login = vi.fn();
    renderLogin(authValue({ login }));
    await userEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(screen.getByRole("alert").textContent).toContain(
      "请输入用户名和密码",
    );
    expect(login).not.toHaveBeenCalled();
  });

  it("trims credentials, logs in, and returns to the original page", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    renderLogin(authValue({ login }), {
      pathname: "/login",
      state: { from: "/bookshelf" },
    });

    await userEvent.type(screen.getByLabelText("用户名"), "  alice  ");
    await userEvent.type(screen.getByLabelText("密码"), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(login).toHaveBeenCalledWith("alice", "secret-pass");
    expect(await screen.findByText("bookshelf-marker")).toBeTruthy();
  });

  it("does not redirect to an external return path", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    renderLogin(authValue({ login }), {
      pathname: "/login",
      state: { from: "//evil.example/steal" },
    });

    await userEvent.type(screen.getByLabelText("用户名"), "alice");
    await userEvent.type(screen.getByLabelText("密码"), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(await screen.findByText("home-marker")).toBeTruthy();
  });

  it("explains invalid credentials and rate limiting", async () => {
    const login = vi
      .fn()
      .mockRejectedValueOnce(new ApiRequestError(401, "unauthorized"))
      .mockRejectedValueOnce(new ApiRequestError(429, "too many"));
    renderLogin(authValue({ login }));

    await userEvent.type(screen.getByLabelText("用户名"), "alice");
    await userEvent.type(screen.getByLabelText("密码"), "wrong");
    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(screen.getByRole("alert").textContent).toContain("用户名或密码错误");

    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(screen.getByRole("alert").textContent).toContain("尝试次数过多");
  });

  it("skips the form when the user is already logged in", () => {
    renderLogin(
      authValue({
        isAuthenticated: true,
        user: { id: 1, user_id: 1, username: "alice", role: "user" },
      }),
    );
    expect(screen.getByText("home-marker")).toBeTruthy();
  });

  it("redirects to the setup page when the instance is uninitialized", () => {
    renderLogin(authValue({ session: { initialized: false, user: null } }));
    expect(screen.getByText("setup-page")).toBeTruthy();
  });
});
