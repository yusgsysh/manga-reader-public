import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchAuthSession,
  login as loginRequest,
  loginWithPasskey as loginWithPasskeyRequest,
  register as registerRequest,
  logout as logoutRequest,
  verifyTOTP as verifyTOTPRequest,
  type AuthSession,
} from "../api/auth";
import { setAccessToken, setUnauthorizedHandler } from "../api/client";
import { AUTH_QUERY_KEY, AuthContext, type AuthContextValue } from "./auth";

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  // Password step-up state: present only between a correct password and a
  // successful TOTP/recovery-code verification. Kept in memory (never
  // localStorage) and cleared on logout/cancel.
  const [mfaToken, setMfaToken] = useState<string | null>(null);

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
    onSuccess: (result) => {
      if (result.mfaRequired) {
        setMfaToken(result.mfaToken);
        return;
      }
      setMfaToken(null);
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, {
        initialized: true,
        user: result.user,
      });
    },
  });

  const passkeyLoginMutation = useMutation({
    mutationFn: loginWithPasskeyRequest,
    onSuccess: (identity) => {
      queryClient.setQueryData<AuthSession>(AUTH_QUERY_KEY, {
        initialized: true,
        user: identity.user,
      });
    },
  });

  const verifyTotpMutation = useMutation({
    mutationFn: (code: string) => verifyTOTPRequest(mfaToken ?? "", code),
    onSuccess: (identity) => {
      setMfaToken(null);
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
      setMfaToken(null);
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
      const result = await loginMutation.mutateAsync({ username, password });
      return { mfaRequired: result.mfaRequired };
    },
    [loginMutation],
  );

  const verifyTOTP = useCallback(
    async (code: string) => {
      await verifyTotpMutation.mutateAsync(code);
    },
    [verifyTotpMutation],
  );

  const cancelMfa = useCallback(() => setMfaToken(null), []);

  const register = useCallback(
    async (username: string, password: string) => {
      await registerMutation.mutateAsync({ username, password });
    },
    [registerMutation],
  );

  const loginWithPasskey = useCallback(async () => {
    await passkeyLoginMutation.mutateAsync();
  }, [passkeyLoginMutation]);

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
      isLoggingInWithPasskey: passkeyLoginMutation.isPending,
      isLoggingOut: logoutMutation.isPending,
      isRegistering: registerMutation.isPending,
      isMfaPending: mfaToken !== null,
      isVerifyingTOTP: verifyTotpMutation.isPending,
      login,
      loginWithPasskey,
      verifyTOTP,
      cancelMfa,
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
    passkeyLoginMutation.isPending,
    logoutMutation.isPending,
    registerMutation.isPending,
    mfaToken,
    verifyTotpMutation.isPending,
    login,
    loginWithPasskey,
    verifyTOTP,
    cancelMfa,
    register,
    logout,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
