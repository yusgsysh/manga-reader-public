import {
  useCallback,
  useEffect,
  useMemo,
  type ReactNode,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchAuthSession,
  login as loginRequest,
  logout as logoutRequest,
  type AuthSession,
} from "../api/auth";
import { setAccessToken, setUnauthorizedHandler } from "../api/client";
import { AUTH_QUERY_KEY, AuthContext, type AuthContextValue } from "./auth";

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();

  const sessionQuery = useQuery({
    queryKey: AUTH_QUERY_KEY,
    queryFn: fetchAuthSession,
    // Auth errors are terminal until the user explicitly retries or logs in.
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  const loginMutation = useMutation({
    mutationFn: ({
      tenant,
      username,
      password,
    }: {
      tenant: string;
      username: string;
      password: string;
    }) => loginRequest(tenant, username, password),
    onSuccess: (identity) => {
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, (previous) => ({
        enabled: previous?.enabled ?? true,
        initialized: true,
        user: identity.user,
        tenant: identity.tenant,
      }));
    },
  });

  const logoutMutation = useMutation({
    mutationFn: logoutRequest,
    onSettled: () => {
      setAccessToken(null);
      // Drop all cached user data so a different session cannot see the
      // previous account's bookshelf/progress after logging back in.
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== "auth",
      });
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, (previous) =>
        previous ? { ...previous, user: null, tenant: null } : previous,
      );
    },
  });

  useEffect(() => {
    // A 401 from any API call invalidates the session once; RequireAuth then
    // performs the single redirect to /login.
    setUnauthorizedHandler(() => {
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== "auth",
      });
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, (previous) =>
        previous ? { ...previous, user: null, tenant: null } : previous,
      );
    });
    return () => setUnauthorizedHandler(null);
  }, [queryClient]);

  const login = useCallback(
    async (tenant: string, username: string, password: string) => {
      await loginMutation.mutateAsync({ tenant, username, password });
    },
    [loginMutation],
  );

  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync();
  }, [logoutMutation]);

  const value = useMemo<AuthContextValue>(() => {
    const session = sessionQuery.data ?? null;
    const user = session?.user ?? null;
    const role = user?.role ?? null;
    return {
      session,
      user,
      tenant: session?.tenant ?? null,
      role,
      enabled: session?.enabled ?? false,
      isAuthenticated: user !== null,
      isPlatformAdmin: role === "platform_admin",
      isTenantAdmin: role === "tenant_admin",
      isLoading: sessionQuery.isPending,
      error: sessionQuery.error instanceof Error ? sessionQuery.error : null,
      isLoggingIn: loginMutation.isPending,
      isLoggingOut: logoutMutation.isPending,
      login,
      logout,
      refresh: sessionQuery.refetch,
    };
  }, [
    sessionQuery.data,
    sessionQuery.isPending,
    sessionQuery.error,
    sessionQuery.refetch,
    loginMutation.isPending,
    logoutMutation.isPending,
    login,
    logout,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
