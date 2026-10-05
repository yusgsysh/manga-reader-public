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

// Refreshes progress/bookshelf caches when the backend signals a change
// (local writes on other tabs, rows applied from a sync peer).
function SyncEventBridge() {
  useSyncEvents();
  return null;
}

export default function App() {
  // The tag-translation database is a 1.75 MB payload whose inflate + index
  // build costs ~1 s of main-thread time on low-end devices. Kicking the load
  // off at idle keeps it out of the route transition and first scroll; every
  // consumer still sees the same "loading -> ready" stream.
  useEffect(() => scheduleIdle(() => void tagTranslationService.load()), []);

  return (
    <ErrorBoundary>
      <ThemeProvider>
        <MotionProvider>
          <QueryClientProvider client={queryClient}>
            <SyncEventBridge />
            <LinkProvider component={AppLink}>
              <TooltipProvider>
                <Toasty>
                  <NavigationProvider>
                    <RouterProvider router={router} />
                  </NavigationProvider>
                </Toasty>
              </TooltipProvider>
            </LinkProvider>
          </QueryClientProvider>
        </MotionProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
