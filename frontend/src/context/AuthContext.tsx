import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useKindeAuth } from "@kinde-oss/kinde-auth-react";
import {
  fetchAuthUser,
  logout as logoutRequest,
  type AuthUser,
} from "../api/auth";
import {
  ApiRequestError,
  setAccessToken,
  setAccessTokenProvider,
  setUnauthorizedHandler,
} from "../api/client";
import {
  isE2EAuth,
  isRegistrationEnabled,
  readKindeConfig,
} from "../auth/kinde";
import {
  AUTH_QUERY_KEY,
  AuthContext,
  type AuthContextValue,
  rememberReturnPath,
} from "./auth";

const E2E_USER: AuthUser = {
  id: "00000000-0000-7000-8000-000000000001",
  user_id: "e2e-subject",
  username: "e2e",
  email: "e2e@example.com",
  name: "E2E",
  avatar_url: null,
  role: "admin",
  status: "active",
};

async function loadCurrentUser(): Promise<AuthUser | null> {
  try {
    return await fetchAuthUser();
  } catch (cause) {
    if (cause instanceof ApiRequestError) {
      // No local session yet (or the access token expired): signed out is a
      // normal state, not an authentication error.
      if (cause.status === 401) return null;
      if (cause.status === 403) {
        throw new Error("账号已被禁用，无法访问 Manga Reader");
      }
    }
    throw cause;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const kinde = useKindeAuth();
  const e2e = isE2EAuth();
  const kindeConfig = useMemo(() => readKindeConfig(), []);
  const registrationEnabled = useMemo(() => isRegistrationEnabled(), []);
  const [pendingAction, setPendingAction] = useState<"login" | "register" | null>(null);

  // The SDK is the only owner of the access token. The API client asks it for
  // a token per request; when the current token is gone we ask the SDK to
  // refresh rather than implementing our own refresh loop.
  useEffect(() => {
    if (e2e) return;
    setAccessTokenProvider(async () => {
      // Prefer the SDK's current access token even when the SDK has not yet
      // flipped isAuthenticated (the token is the authoritative credential;
      // /api/auth/me decides whether the local account is usable).
      const current = await kinde.getAccessToken();
      if (current) return current;
      if (!kinde.isAuthenticated || !kindeConfig) return null;
      const refreshed = await kinde.refreshToken({
        domain: kindeConfig.domain,
        clientId: kindeConfig.clientId,
      });
      if (refreshed?.success) {
        return (await kinde.getAccessToken()) ?? null;
      }
      return null;
    });
    return () => setAccessTokenProvider(null);
  }, [e2e, kinde, kindeConfig]);

  const sessionQuery = useQuery({
    queryKey: AUTH_QUERY_KEY,
    queryFn: loadCurrentUser,
    // Probe /api/auth/me once the SDK has finished initializing. Do not gate
    // on isAuthenticated: some SDK states briefly report false after a
    // successful code exchange, and the backend is the source of truth for
    // whether a token maps to a usable local account.
    enabled: !e2e && !kinde.isLoading,
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  // A 401 from any API call invalidates the local identity once; RequireAuth
  // performs the single redirect to /login.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      setAccessToken(null);
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== "auth",
      });
      queryClient.setQueryData(AUTH_QUERY_KEY, null);
    });
    return () => setUnauthorizedHandler(null);
  }, [queryClient]);

  const login = useCallback(
    async (returnTo?: string) => {
      rememberReturnPath(returnTo);
      setPendingAction("login");
      try {
        await kinde.login();
      } finally {
        setPendingAction(null);
      }
    },
    [kinde],
  );

  const register = useCallback(
    async (returnTo?: string) => {
      if (!registrationEnabled) {
        throw new Error("registration is disabled");
      }
      rememberReturnPath(returnTo);
      setPendingAction("register");
      try {
        await kinde.register();
      } finally {
        setPendingAction(null);
      }
    },
    [kinde, registrationEnabled],
  );

  const logoutMutation = useMutation({
    mutationFn: async () => {
      if (e2e) return;
      try {
        await logoutRequest();
      } catch {
        // The local session bump is best-effort; the SDK logout still clears
        // the browser's tokens and the redirect below ends the session.
      }
    },
    onSettled: () => {
      setAccessToken(null);
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== "auth",
      });
      queryClient.setQueryData(AUTH_QUERY_KEY, null);
    },
  });

  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync();
    if (!e2e) {
      await kinde.logout(
        kindeConfig ? { redirectUrl: kindeConfig.logoutUri } : undefined,
      );
    }
  }, [e2e, kinde, kindeConfig, logoutMutation]);

  const value = useMemo<AuthContextValue>(() => {
    const user = e2e ? E2E_USER : (sessionQuery.data ?? null);
    const role = user?.role ?? null;
    const loading = e2e ? false : kinde.isLoading || sessionQuery.isPending;
    const error =
      sessionQuery.error instanceof Error ? sessionQuery.error : null;
    return {
      user,
      role,
      isAuthenticated: e2e || user !== null,
      isAdmin: role === "admin",
      isLoading: loading,
      error,
      isLoggingIn: pendingAction === "login",
      isLoggingOut: logoutMutation.isPending,
      isRegistering: pendingAction === "register",
      registrationEnabled,
      login,
      register,
      logout,
      refresh: sessionQuery.refetch,
    };
  }, [
    e2e,
    kinde.isLoading,
    sessionQuery.data,
    sessionQuery.isPending,
    sessionQuery.error,
    sessionQuery.refetch,
    pendingAction,
    logoutMutation.isPending,
    registrationEnabled,
    login,
    register,
    logout,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
