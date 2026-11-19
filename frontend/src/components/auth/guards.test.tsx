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
import { AuthContext, type AuthContextValue } from "../../context/auth";
import { RequireAuth } from "./RequireAuth";
import { LoginPage } from "../../pages/LoginPage";

const user = {
  id: "00000000-0000-7000-8000-000000000001",
  user_id: "kp_alice",
  username: "kinde_1234567890abcdef",
  email: "alice@example.com",
  name: "Alice",
  avatar_url: null,
  role: "user",
  status: "active",
};

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
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
          <Route path="/bookshelf" element={<div>bookshelf-marker</div>} />
          <Route path="/" element={<div>home-marker</div>} />
          <Route path="/login" element={<LoginProbe />} />
        </Routes>
      </MemoryRouter>
    </AuthContext.Provider>,
  );
}

describe("RequireAuth", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it("redirects an unauthenticated visitor to /login and preserves the destination", () => {
    renderGuard(authValue(), "/private?tab=all");
    expect(screen.getByText("login-page")).toBeTruthy();
    expect(screen.getByTestId("from").textContent).toBe("/private?tab=all");
    expect(screen.queryByText("private-content")).toBeNull();
  });

  it("renders the page when authenticated", () => {
    renderGuard(authValue({ isAuthenticated: true, user }), "/private");
    expect(screen.getByText("private-content")).toBeTruthy();
  });

  it("shows a retryable error instead of redirecting on a session error", () => {
    const refresh = vi.fn();
    renderGuard(authValue({ error: new Error("offline"), refresh }), "/private");
    expect(screen.getByText(/offline/)).toBeTruthy();
    screen.getByRole("button").click();
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("restores the destination saved before the Kinde redirect", () => {
    sessionStorage.setItem("manga-reader:return-to", "/bookshelf");
    renderGuard(authValue({ isAuthenticated: true, user }), "/private");
    expect(screen.getByText("bookshelf-marker")).toBeTruthy();
  });
});

function renderLogin(
  value: AuthContextValue,
  initialEntry: { pathname: string; state?: unknown; search?: string } = {
    pathname: "/login",
  },
) {
  return render(
    <AuthContext.Provider value={value}>
      <ThemeProvider>
        <MemoryRouter
          initialEntries={[
            {
              pathname: initialEntry.pathname,
              search: initialEntry.search,
              state: initialEntry.state,
            },
          ]}
        >
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home-marker</div>} />
            <Route path="/bookshelf" element={<div>bookshelf-marker</div>} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>
    </AuthContext.Provider>,
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
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

  it("starts the Kinde login flow with the original destination", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    renderLogin(authValue({ login }), {
      pathname: "/login",
      state: { from: "/bookshelf" },
    });

    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(login).toHaveBeenCalledWith("/bookshelf");
  });

  it("offers registration for first-time users", async () => {
    const register = vi.fn().mockResolvedValue(undefined);
    renderLogin(authValue({ register }));

    await userEvent.click(
      screen.getByRole("button", { name: "注册新账号" }),
    );
    expect(register).toHaveBeenCalledWith("/");
  });

  it("does not redirect to an external return path", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    renderLogin(authValue({ login }), {
      pathname: "/login",
      state: { from: "//evil.example/steal" },
    });

    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(login).toHaveBeenCalledWith("/");
  });

  it("explains a Kinde callback error", () => {
    renderLogin(authValue(), {
      pathname: "/login",
      search: "?error=access_denied&error_description=User+cancelled",
    });
    expect(screen.getByRole("alert").textContent).toContain("User cancelled");
  });

  it("skips the form when the user is already logged in", () => {
    renderLogin(authValue({ isAuthenticated: true, user }));
    expect(screen.getByText("home-marker")).toBeTruthy();
  });
});
