import { useCallback, useRef, useState } from "react";
import { useKumoToastManager } from "@cloudflare/kumo";
import {
  fetchGalleryPages,
  packZip,
  sanitizeFilename,
  saveBlob,
} from "../lib/zip";

export type ZipStatus =
  | "idle"
  | "downloading"
  | "zipping"
  | "success"
  | "error"
  | "cancelled";

export interface ZipState {
  status: ZipStatus;
  done: number;
  total: number;
  error?: string;
}

const IDLE_STATE: ZipState = { status: "idle", done: 0, total: 0 };

function isTerminal(status: ZipStatus): boolean {
  return status === "success" || status === "error" || status === "cancelled";
}

function waitForPaint(): Promise<void> {
  return new Promise((resolve) =>
    requestAnimationFrame(() => setTimeout(resolve, 0)),
  );
}

export function useGalleryZip() {
  const [state, setState] = useState<ZipState>(IDLE_STATE);
  const controllerRef = useRef<AbortController | null>(null);
  const toast = useKumoToastManager();

  const cancel = useCallback(() => {
    const controller = controllerRef.current;
    if (!controller) return;
    controller.abort();
    setState((prev) =>
      isTerminal(prev.status) ? prev : { ...prev, status: "cancelled" },
    );
  }, []);

  const start = useCallback(
    async (title: string, pageUrls: string[]) => {
      if (controllerRef.current || pageUrls.length === 0) return;
      const controller = new AbortController();
      controllerRef.current = controller;
      setState({ status: "downloading", done: 0, total: pageUrls.length });

      const filename = `${sanitizeFilename(title)}.zip`;
      try {
        const files = await fetchGalleryPages({
          pageUrls,
          signal: controller.signal,
          onProgress: (done, total) =>
            setState({ status: "downloading", done, total }),
        });
        setState({ status: "zipping", done: pageUrls.length, total: pageUrls.length });
        await waitForPaint();
        if (controller.signal.aborted) {
          throw new DOMException("Aborted", "AbortError");
        }
        const blob = packZip(files);
        saveBlob(blob, filename);
        setState({ status: "success", done: pageUrls.length, total: pageUrls.length });
        toast.add({ title: "ZIP 已生成", description: filename, variant: "success" });
      } catch (error) {
        const aborted =
          (error instanceof DOMException && error.name === "AbortError") ||
          controller.signal.aborted;
        if (aborted) {
          setState((prev) => ({ ...prev, status: "cancelled" }));
        } else {
          const message = error instanceof Error ? error.message : String(error);
          setState((prev) => ({ ...prev, status: "error", error: message }));
          toast.add({
            title: "下载 ZIP 失败",
            description: message,
            variant: "error",
          });
        }
      } finally {
        controllerRef.current = null;
      }
    },
    [toast],
  );

  return {
    status: state.status,
    done: state.done,
    total: state.total,
    error: state.error,
    running: state.status === "downloading" || state.status === "zipping",
    start,
    cancel,
  };
}
