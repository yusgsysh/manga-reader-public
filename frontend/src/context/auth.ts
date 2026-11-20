import { createContext, useContext } from "react";
import type { AuthSession, AuthUser } from "../api/auth";

export const AUTH_QUERY_KEY = ["auth", "session"] as const;

export interface AuthContextValue {
  session: AuthSession | null;
  user: AuthUser | null;
  role: string | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  isLoading: boolean;
  error: Error | null;
  isLoggingIn: boolean;
  isLoggingInWithPasskey: boolean;
  isLoggingOut: boolean;
  isRegistering: boolean;
  /** Password accepted, waiting for a TOTP or recovery code. */
  isMfaPending: boolean;
  isVerifyingTOTP: boolean;
  login: (
    username: string,
    password: string,
  ) => Promise<{ mfaRequired: boolean }>;
  loginWithPasskey: () => Promise<void>;
  verifyTOTP: (code: string) => Promise<void>;
  cancelMfa: () => void;
  register: (username: string, password: string) => Promise<void>;
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
