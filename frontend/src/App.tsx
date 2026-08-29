import { useEffect } from "react";
import { RouterProvider } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toasty, TooltipProvider } from "@cloudflare/kumo";
import { ErrorBoundary } from "./components/common/ErrorBoundary";
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

export default function App() {
  useEffect(() => {
    tagTranslationService.load();
  }, []);

  return (
    <ErrorBoundary>
      <ThemeProvider>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <Toasty>
              <RouterProvider router={router} />
            </Toasty>
          </TooltipProvider>
        </QueryClientProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
