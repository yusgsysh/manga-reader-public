import { zipSync } from "fflate";
import { apiBlob, ApiRequestError } from "../api/client";

const CACHED_IMAGE_PATH = "/api/cached-image";
const MAX_ATTEMPTS = 3;
const RETRY_DELAY_MS = 500;
const MAX_FILENAME_LENGTH = 100;

export function sanitizeFilename(title: string, fallback = "gallery"): string {
  const cleaned = title
    .replace(/[\\/:*?"<>|]/g, " ")
    // oxlint-disable-next-line no-control-regex
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, MAX_FILENAME_LENGTH)
    .trim();
  return cleaned || fallback;
}

export function extFromContentType(contentType: string | null, pageURL: string): string {
  switch (contentType?.split(";")[0].trim().toLowerCase()) {
    case "image/jpeg":
    case "image/jpg":
      return ".jpg";
    case "image/png":
      return ".png";
    case "image/webp":
      return ".webp";
    case "image/gif":
      return ".gif";
    default:
      break;
  }
  try {
    const pathname = new URL(pageURL).pathname;
    const match = pathname.match(/\.(jpe?g|png|webp|gif)$/i);
    if (match) return match[1].toLowerCase() === "jpeg" ? ".jpg" : `.${match[1].toLowerCase()}`;
  } catch {
    // ignore invalid URL
  }
  return ".jpg";
}

function isRetryable(error: unknown): boolean {
  if (error instanceof DOMException && error.name === "AbortError") return false;
  if (error instanceof ApiRequestError) return error.status >= 500;
  return true;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function fetchPageBlob(pageURL: string, signal: AbortSignal): Promise<Blob> {
  let lastError: unknown;
  for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    try {
      return await apiBlob(CACHED_IMAGE_PATH, { url: pageURL }, { signal });
    } catch (error) {
      if (!isRetryable(error)) throw error;
      lastError = error;
      if (attempt < MAX_ATTEMPTS - 1) await delay(RETRY_DELAY_MS);
    }
  }
  throw lastError;
}

export interface FetchGalleryPagesOptions {
  pageUrls: string[];
  signal: AbortSignal;
  onProgress?: (done: number, total: number) => void;
}

export type GalleryZipFiles = Record<string, Uint8Array>;

export async function fetchGalleryPages(
  options: FetchGalleryPagesOptions,
): Promise<GalleryZipFiles> {
  const { pageUrls, signal, onProgress } = options;
  const total = pageUrls.length;
  const files: GalleryZipFiles = {};

  for (let index = 0; index < total; index++) {
    const pageURL = pageUrls[index];
    const blob = await fetchPageBlob(pageURL, signal);
    const ext = extFromContentType(blob.type, pageURL);
    const name = `${String(index + 1).padStart(3, "0")}${ext}`;
    files[name] = new Uint8Array(await blob.arrayBuffer());
    onProgress?.(index + 1, total);
  }

  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  return files;
}

export function packZip(files: GalleryZipFiles): Blob {
  const bytes = zipSync(files, { level: 0 });
  return new Blob([bytes.buffer as ArrayBuffer], { type: "application/zip" });
}

export function saveBlob(blob: Blob, filename: string): void {
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = filename;
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(href), 10_000);
}
