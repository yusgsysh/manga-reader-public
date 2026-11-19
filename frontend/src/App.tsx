import { forwardRef, useEffect, type ReactNode } from "react";
import { Link as RouterLink, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  LinkProvider,
  Toasty,
  TooltipProvider,
  type LinkComponentProps,
} from "@cloudflare/kumo";
import {
  KindeContext,
  KindeProvider,
  type KindeContextProps,
} from "@kinde-oss/kinde-auth-react";
import { ErrorBoundary } from "./components/common/ErrorBoundary";
import { Logo } from "./components/ui";
import { AuthProvider } from "./context/AuthContext";
import { useAuth } from "./context/auth";
import { NavigationProvider } from "./context/NavigationContext";
import { useSyncEvents } from "./hooks/useSyncEvents";
import { isE2EAuth, readKindeConfig } from "./auth/kinde";
import { GLOBAL_STALE_TIME } from "./lib/cacheConfig";
import { scheduleIdle } from "./lib/idle";
import { MotionProvider } from "./lib/motion";
import { ThemeProvider } from "./lib/theme";
import { tagTranslationService } from "./services/tagTranslation";
import { router } from "./router";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: GLOBAL_STALE_TIME,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(
  ({ href, ...rest }, ref) => <RouterLink ref={ref} to={href ?? ""} {...rest} />,
);
AppLink.displayName = "AppLink";

// Background work that only makes sense once a user is signed in: subscribe to
// the local change stream (SSE) so cross-tab/peer writes invalidate caches, and
// warm the tag-translation database at idle so tag-heavy pages don't flash
// untranslated labels.
function AuthenticatedEffects() {
  const { isAuthenticated } = useAuth();
  useSyncEvents(isAuthenticated);

  useEffect(() => {
    if (!isAuthenticated) return;
    return scheduleIdle(() => void tagTranslationService.load());
  }, [isAuthenticated]);

  return null;
}

function MissingKindeConfig() {
  return (
    <div className="login-backdrop flex min-h-dvh items-center justify-center px-4 text-kumo-default">
      <div className="auth-panel max-w-md p-6 text-center">
        <Logo className="mx-auto [&_svg]:size-9" />
        <h1 className="mt-5 text-xl font-semibold">认证尚未配置</h1>
        <p className="mt-3 text-sm leading-6 text-kumo-subtle">
          请设置 VITE_KINDE_DOMAIN、VITE_KINDE_CLIENT_ID 与
          VITE_KINDE_AUDIENCE，然后重新构建前端。
        </p>
      </div>
    </div>
  );
}

function E2EKindeContext({ children }: { children: ReactNode }) {
  const value = {
    isLoading: false,
    isAuthenticated: true,
    user: undefined,
    getAccessToken: async () => "e2e-access-token",
    getIdToken: async () => undefined,
    getToken: async () => "e2e-access-token",
    refreshToken: async () => ({ success: true }),
    login: async () => undefined,
    register: async () => undefined,
    logout: async () => undefined,
  } as unknown as KindeContextProps;
  return <KindeContext.Provider value={value}>{children}</KindeContext.Provider>;
}

function AuthRoot({ children }: { children: ReactNode }) {
  if (isE2EAuth()) {
    return <E2EKindeContext>{children}</E2EKindeContext>;
  }
  const config = readKindeConfig();
  if (!config) return <MissingKindeConfig />;
  return (
    <KindeProvider
      domain={config.domain}
      clientId={config.clientId}
      redirectUri={config.redirectUri}
      logoutUri={config.logoutUri}
      audience={config.audience}
      scope="openid profile email offline"
      // Render immediately and let AuthContext own the loading screen, so the
      // app never flashes a blank document while Kinde restores a session.
      forceChildrenRender
    >
      {children}
    </KindeProvider>
  );
}

export default function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <MotionProvider>
          <AuthRoot>
            <QueryClientProvider client={queryClient}>
              <AuthProvider>
                <AuthenticatedEffects />
                <LinkProvider component={AppLink}>
                  <TooltipProvider>
                    <Toasty>
                      <NavigationProvider>
                        <RouterProvider router={router} />
                      </NavigationProvider>
                    </Toasty>
                  </TooltipProvider>
                </LinkProvider>
              </AuthProvider>
            </QueryClientProvider>
          </AuthRoot>
        </MotionProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
