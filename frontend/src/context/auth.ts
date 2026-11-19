import { createContext, useContext } from "react";
import type { AuthUser } from "../api/auth";

export const AUTH_QUERY_KEY = ["auth", "me"] as const;

const RETURN_TO_KEY = "manga-reader:return-to";

/** Only allow same-origin paths as a post-login destination. */
export function safeReturnPath(value: unknown): string {
  if (
    typeof value === "string" &&
    value.startsWith("/") &&
    !value.startsWith("//") &&
    !value.startsWith("/login")
  ) {
    return value;
  }
  return "/";
}

export function rememberReturnPath(value: unknown): void {
  try {
    sessionStorage.setItem(RETURN_TO_KEY, safeReturnPath(value));
  } catch {
    // Private mode / storage disabled: falling back to "/" is harmless.
  }
}

/** Reads the destination saved before the Kinde redirect. */
export function readReturnPath(): string | null {
  try {
    const value = sessionStorage.getItem(RETURN_TO_KEY);
    return value === null ? null : safeReturnPath(value);
  } catch {
    return null;
  }
}

export function clearReturnPath(): void {
  try {
    sessionStorage.removeItem(RETURN_TO_KEY);
  } catch {
    // Nothing to clear.
  }
}

export interface AuthContextValue {
  user: AuthUser | null;
  role: string | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  isLoading: boolean;
  error: Error | null;
  isLoggingIn: boolean;
  isLoggingOut: boolean;
  isRegistering: boolean;
  login: (returnTo?: string) => Promise<void>;
  register: (returnTo?: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<unknown>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return context;
}
