import { forwardRef, useEffect } from "react";
import { Link as RouterLink, RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  LinkProvider,
  Toasty,
  TooltipProvider,
  type LinkComponentProps,
} from "@cloudflare/kumo";
import { ErrorBoundary } from "./components/common/ErrorBoundary";
import { AuthProvider } from "./context/AuthContext";
import { useAuth } from "./context/auth";
import { NavigationProvider } from "./context/NavigationContext";
import { useSyncEvents } from "./hooks/useSyncEvents";
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
// untranslated labels. Neither runs on the login page, which has no synced data
// and no tags — otherwise a cold visit would still pull the 1.75 MB db.text.js
// and open a doomed 401 SSE stream.
function AuthenticatedEffects() {
  const { isAuthenticated } = useAuth();
  useSyncEvents(isAuthenticated);

  useEffect(() => {
    if (!isAuthenticated) return;
    // `load()` is idempotent, so this prefetch never races the on-demand
    // `load()` inside useTagTranslation; both share the same promise.
    return scheduleIdle(() => void tagTranslationService.load());
  }, [isAuthenticated]);

  return null;
}

export default function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <MotionProvider>
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
        </MotionProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
