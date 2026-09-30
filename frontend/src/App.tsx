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
import { MotionProvider } from "./lib/motion";
import { ThemeProvider } from "./lib/theme";
import { tagTranslationService } from "./services/tagTranslation";
import { router } from "./router";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(
  ({ href, ...rest }, ref) => <RouterLink ref={ref} to={href ?? ""} {...rest} />,
);
AppLink.displayName = "AppLink";

export default function App() {
  useEffect(() => {
    tagTranslationService.load();
  }, []);

  return (
    <ErrorBoundary>
      <ThemeProvider>
        <MotionProvider>
          <QueryClientProvider client={queryClient}>
            <LinkProvider component={AppLink}>
              <TooltipProvider>
                <Toasty>
                  <RouterProvider router={router} />
                </Toasty>
              </TooltipProvider>
            </LinkProvider>
          </QueryClientProvider>
        </MotionProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
