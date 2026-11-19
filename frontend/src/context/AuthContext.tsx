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
import { isE2EAuth, readKindeConfig } from "../auth/kinde";
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
  const [pendingAction, setPendingAction] = useState<"login" | "register" | null>(null);

  // The SDK is the only owner of the access token. The API client asks it for
  // a token per request; when the current token is gone we ask the SDK to
  // refresh rather than implementing our own refresh loop.
  useEffect(() => {
    if (e2e) return;
    setAccessTokenProvider(async () => {
      const current = await kinde.getAccessToken();
      if (current) return current;
      if (!kindeConfig) return null;
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
    enabled: !e2e && !kinde.isLoading && kinde.isAuthenticated,
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  useEffect(() => {
    if (e2e || !kinde.isAuthenticated) {
      queryClient.setQueryData(AUTH_QUERY_KEY, null);
    }
  }, [e2e, kinde.isAuthenticated, queryClient]);

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
      rememberReturnPath(returnTo);
      setPendingAction("register");
      try {
        await kinde.register();
      } finally {
        setPendingAction(null);
      }
    },
    [kinde],
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
    const loading = e2e
      ? false
      : kinde.isLoading ||
        (kinde.isAuthenticated && sessionQuery.isPending);
    const error =
      sessionQuery.error instanceof Error ? sessionQuery.error : null;
    return {
      user,
      role,
      isAuthenticated: e2e || (kinde.isAuthenticated && user !== null),
      isAdmin: role === "admin",
      isLoading: loading,
      error,
      isLoggingIn: pendingAction === "login",
      isLoggingOut: logoutMutation.isPending,
      isRegistering: pendingAction === "register",
      login,
      register,
      logout,
      refresh: sessionQuery.refetch,
    };
  }, [
    e2e,
    kinde.isLoading,
    kinde.isAuthenticated,
    sessionQuery.data,
    sessionQuery.isPending,
    sessionQuery.error,
    sessionQuery.refetch,
    pendingAction,
    logoutMutation.isPending,
    login,
    register,
    logout,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
