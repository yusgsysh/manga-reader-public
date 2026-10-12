import { useKumoToastManager } from "@cloudflare/kumo";

export type ToastVariant = "default" | "success" | "error" | "warning" | "info";

export interface ToastOptions {
  title: string;
  description?: string;
  variant?: ToastVariant;
}

/**
 * Unified toast entry point for the whole app. Every notification goes
 * through this hook so the style stays identical everywhere, and multiple
 * toasts stack consistently: kumo's viewport renders the newest toast in
 * front while older ones recede behind it (scaled down, peeking above).
 */
export function useToast() {
  const toast = useKumoToastManager();

  const show = (options: ToastOptions) => {
    toast.add(options);
  };

  const success = (title: string, description?: string) => {
    show({ title, description, variant: "success" });
  };

  const info = (title: string, description?: string) => {
    show({ title, description, variant: "info" });
  };

  /** Error toast; extracts `.message` from Error-like values. */
  const error = (title: string, cause?: unknown) => {
    show({
      title,
      description:
        cause instanceof Error
          ? cause.message
          : cause == null
            ? undefined
            : String(cause),
      variant: "error",
    });
  };

  return { show, success, info, error };
}
