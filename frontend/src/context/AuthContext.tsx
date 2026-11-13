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
  register as registerRequest,
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
      username,
      password,
    }: {
      username: string;
      password: string;
    }) => loginRequest(username, password),
    onSuccess: (identity) => {
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, {
        initialized: true,
        user: identity.user,
      });
    },
  });

  const registerMutation = useMutation({
    mutationFn: ({
      username,
      password,
    }: {
      username: string;
      password: string;
    }) => registerRequest(username, password),
    onSuccess: (identity) => {
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, {
        initialized: true,
        user: identity.user,
      });
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
        previous ? { ...previous, user: null } : previous,
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
        previous ? { ...previous, user: null } : previous,
      );
    });
    return () => setUnauthorizedHandler(null);
  }, [queryClient]);

  const login = useCallback(
    async (username: string, password: string) => {
      await loginMutation.mutateAsync({ username, password });
    },
    [loginMutation],
  );

  const register = useCallback(
    async (username: string, password: string) => {
      await registerMutation.mutateAsync({ username, password });
    },
    [registerMutation],
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
      role,
      isAuthenticated: user !== null,
      isAdmin: role === "admin",
      isLoading: sessionQuery.isPending,
      error: sessionQuery.error instanceof Error ? sessionQuery.error : null,
      isLoggingIn: loginMutation.isPending,
      isLoggingOut: logoutMutation.isPending,
      isRegistering: registerMutation.isPending,
      login,
      register,
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
    registerMutation.isPending,
    login,
    register,
    logout,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
