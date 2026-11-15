/**
 * The book reading view, built on grabfold.
 *
 * Comimi's own reader keeps the paged/sliding modes; this viewer only mounts
 * when the reader's page-turn animation is set to "书本". It hands grabfold the
 * comic's images as a book of sheets (see `lib/bookPages`), curling the turning
 * leaf on the GPU through `grabfold/webgl` and falling back to grabfold's DOM
 * fold wherever WebGL is unavailable — including when the context cannot be
 * created at runtime.
 *
 * The comic page index is the only reading position this component stores
 * (0-based). grabfold's position is derived from it, and every action —
 * gesture, click zone, key, toolbar, page jump — funnels through the same
 * imperative handle, so the book and the app chrome cannot disagree.
 *
 * Images come from `lib/pageImages`, which resolves each page through the
 * reader's existing image endpoint and hands the same decoded element to the
 * DOM page and to the WebGL texture.
 */

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { GrabfoldBook, type GrabfoldBookHandle, type PageRef } from "grabfold/react";
import { webglFold } from "grabfold/webgl";
import {
  bookBinding,
  bookPageMap,
  clampPage,
  pageForPosition,
  pageForRef,
  pageWindow,
  positionForPage,
  turnForKey,
  turnForSide,
  type BookPageMap,
  type PageTurn,
  type ReadingDirection,
} from "../../lib/bookPages";
import { PageImageCache } from "../../lib/pageImages";
import { fitPageTexture } from "../../lib/pageTexture";

/** One page's width over its height, until the first image says otherwise. */
const FALLBACK_PAGE_RATIO = 0.7;

/** How long the chrome stays up after the last pointer or key activity. */
const OVERLAY_HIDE_MS = 3000;

/** How far around the reading position pages are preloaded and pinned. */
const WINDOW_BEFORE = 2;
const WINDOW_AFTER = 2;

/** GPU textures kept by the curl renderer (a page is a full-screen image). */
const TEXTURE_CACHE = 6;

export interface BookFlipViewerProps {
  /** Comic pages, 0-based count. */
  total: number;
  /** Comic page to open at (0-based). Read once, at mount. */
  initialPage: number;
  readingDirection: ReadingDirection;
  pageTurnMode: "single" | "spread";
  hasCover: boolean;
  autoPageTurn: boolean;
  autoPageTurnIntervalMs: number;
  reducedMotion: boolean;
  /** Resolves a 0-based comic page to the reader's image URL. */
  resolvePageSrc: (pageIndex: number) => Promise<string>;
  onPageChange?: (pageIndex: number) => void;
  onOverlayChange?: (visible: boolean) => void;
  onToggleFullscreen?: () => void;
  onToggleAutoPageTurn?: () => void;
  onTogglePageTurnMode?: () => void;
  onToggleCover?: () => void;
}

export interface BookFlipViewerHandle {
  /** Jumps the book to a comic page (animated). */
  goToPage(pageIndex: number): void;
  next(): void;
  previous(): void;
  /** The comic page the book is reading from. */
  getCurrentPage(): number;
  toggleAutoPageTurn(): void;
}

interface BookView {
  position: number;
  /** Identifies the page map this position belongs to. */
  signature: string;
}

function signatureOf(map: BookPageMap): string {
  return `${map.layout}:${map.hasCover ? 1 : 0}:${map.pages}`;
}

/** A small budget on phones and low-memory devices. */
function imageBudget(): {
  maxBytes: number;
  maxImages: number;
  concurrency: number;
} {
  if (typeof window === "undefined") {
    return { maxBytes: 64 * 1024 * 1024, maxImages: 8, concurrency: 3 };
  }
  const mobile = window.matchMedia("(max-width: 767px)").matches;
  const memory = (navigator as Navigator & { deviceMemory?: number }).deviceMemory;
  const low = mobile || (typeof memory === "number" && memory <= 4);
  return low
    ? { maxBytes: 48 * 1024 * 1024, maxImages: 6, concurrency: 2 }
    : { maxBytes: 112 * 1024 * 1024, maxImages: 10, concurrency: 3 };
}

/**
 * Whether a curl can be drawn, without keeping the probe's WebGL context
 * alive: browsers allow only a handful of live contexts per page.
 */
function canCurl(): boolean {
  if (typeof document === "undefined") return false;
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2") ?? canvas.getContext("webgl");
  if (!gl) return false;
  gl.getExtension("WEBGL_lose_context")?.loseContext();
  return true;
}

function maxPixelRatio(): number {
  if (typeof window === "undefined") return 1;
  return Math.min(window.devicePixelRatio || 1, 2);
}

function isTypingTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    target.closest(
      "input, select, textarea, [contenteditable='true'], [role='dialog']",
    ) !== null
  );
}

export const BookFlipViewer = forwardRef<
  BookFlipViewerHandle,
  BookFlipViewerProps
>(function BookFlipViewer(props, ref) {
  const {
    total,
    readingDirection,
    pageTurnMode,
    hasCover,
    autoPageTurn,
    autoPageTurnIntervalMs,
    reducedMotion,
    resolvePageSrc,
    onPageChange,
    onOverlayChange,
    onToggleFullscreen,
    onToggleAutoPageTurn,
    onTogglePageTurnMode,
    onToggleCover,
  } = props;

  const layout = pageTurnMode === "spread" ? "spread" : "single";
  const map = useMemo(
    () => bookPageMap(total, layout, hasCover),
    [total, layout, hasCover],
  );
  const signature = signatureOf(map);

  /* --------------------------------------------------- page & position */

  // The comic page is authoritative; grabfold's position is only ever derived
  // from it. A layout or cover change computes the new position during render,
  // so the rebuilt book never opens on the wrong spread for a frame.
  const [currentPage, setCurrentPage] = useState(() =>
    clampPage(props.initialPage, map.pages),
  );
  const [view, setView] = useState<BookView>(() => ({
    position: positionForPage(map, currentPage),
    signature,
  }));
  if (view.signature !== signature) {
    setView({
      position: positionForPage(map, currentPage),
      signature,
    });
  }

  // Values read from event handlers and from grabfold's async callbacks. They
  // are synced after commit, never during render.
  const mapRef = useRef(map);
  const positionRef = useRef(view.position);
  useEffect(() => {
    mapRef.current = map;
  }, [map]);
  useEffect(() => {
    positionRef.current = view.position;
  }, [view.position]);

  const bookRef = useRef<GrabfoldBookHandle>(null);

  /* ---------------------------------------------------------- images */

  const images = useMemo(() => {
    const budget = imageBudget();
    return new PageImageCache({ resolve: resolvePageSrc, ...budget });
  }, [resolvePageSrc]);
  const imagesRef = useRef(images);
  useEffect(() => {
    imagesRef.current = images;
  }, [images]);
  // Release decoded pages with the view; the cache is rebuilt per mount.
  useEffect(() => () => images.clear(), [images]);

  const [pageRatio, setPageRatio] = useState(FALLBACK_PAGE_RATIO);
  const pageRatioRef = useRef(pageRatio);
  useEffect(() => {
    pageRatioRef.current = pageRatio;
  }, [pageRatio]);
  const ratioSettled = useRef(false);
  const handlePageSize = useCallback((ratio: number) => {
    // The first real page defines the book's shape. A later double-page
    // spread must not resize the book under the reader's hands.
    if (ratioSettled.current) return;
    if (!Number.isFinite(ratio) || ratio < 0.4 || ratio > 1.1) return;
    ratioSettled.current = true;
    pageRatioRef.current = ratio;
    setPageRatio(ratio);
  }, []);

  // Keep the pages around the reading position decoded and pinned.
  useEffect(() => {
    images.retain(pageWindow(map, currentPage, WINDOW_BEFORE, WINDOW_AFTER));
  }, [images, map, currentPage, view.position]);

  const handlePositionChange = useCallback(
    (next: number) => {
      const current = mapRef.current;
      const page = pageForPosition(current, next);
      setCurrentPage(page);
      setView({ position: next, signature: signatureOf(current) });
      onPageChange?.(page);
    },
    [onPageChange],
  );

  /* ------------------------------------------------------------- webgl */

  // One WebGL renderer for the life of the viewer: it owns a GPU context, and
  // grabfold would drop and rebuild it whenever the factory identity changed.
  const [foldRenderer] = useState(() => {
    if (!canCurl()) return null;
    // The texture callback is created with the renderer and called only while
    // a turn runs, so it reads the live map and cache through their refs
    // rather than through the render that happened to create it.
    /* oxlint-disable react/refs */
    const texture = (page: PageRef) => {
      const index = pageForRef(mapRef.current, page);
      if (index === null) return null;
      return imagesRef.current
        .load(index)
        .then((image) => fitPageTexture(image, pageRatioRef.current))
        .catch(() => null);
    };
    /* oxlint-enable react/refs */
    try {
      return webglFold({
        texture,
        radius: 0.12,
        segments: 48,
        maxPixelRatio: maxPixelRatio(),
        cache: TEXTURE_CACHE,
      });
    } catch {
      // WebGL could not even be prepared: grabfold's DOM fold takes over.
      return null;
    }
  });

  /* ---------------------------------------------------------- controls */

  const goToPosition = useCallback((position: number) => {
    const current = mapRef.current;
    setView({
      position: Math.min(current.last, Math.max(current.first, position)),
      signature: signatureOf(current),
    });
  }, []);

  const goToPage = useCallback(
    (pageIndex: number) => {
      const current = mapRef.current;
      const page = clampPage(pageIndex, current.pages);
      const target = positionForPage(current, page);
      const samePosition = target === positionRef.current;
      setCurrentPage(page);
      goToPosition(target);
      // A turn reports the page it lands on; a jump within the spread on show
      // makes no turn, so it has to report the requested page itself.
      if (samePosition) onPageChange?.(page);
    },
    [goToPosition, onPageChange],
  );

  const turn = useCallback((direction: PageTurn) => {
    if (direction === "next") void bookRef.current?.next();
    else void bookRef.current?.prev();
  }, []);

  useImperativeHandle(
    ref,
    () => ({
      goToPage,
      next: () => turn("next"),
      previous: () => turn("previous"),
      getCurrentPage: () => currentPage,
      toggleAutoPageTurn: () => onToggleAutoPageTurn?.(),
    }),
    [goToPage, turn, currentPage, onToggleAutoPageTurn],
  );

  /* ----------------------------------------------------------- overlay */

  // The chrome starts up for a moment, then follows the reader's activity the
  // way comimi's own overlay does: activity only postpones the hide, so a tap
  // still reaches the page-turn zones.
  const [overlayVisible, setOverlayVisible] = useState(true);
  const overlayRef = useRef(overlayVisible);
  useEffect(() => {
    overlayRef.current = overlayVisible;
  }, [overlayVisible]);

  useEffect(() => {
    onOverlayChange?.(overlayVisible);
  }, [onOverlayChange, overlayVisible]);

  useEffect(() => {
    if (!overlayVisible) return;
    let timer = window.setTimeout(
      () => setOverlayVisible(false),
      OVERLAY_HIDE_MS,
    );
    const hold = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setOverlayVisible(false), OVERLAY_HIDE_MS);
    };
    window.addEventListener("pointermove", hold, { passive: true });
    window.addEventListener("pointerdown", hold, { passive: true });
    window.addEventListener("keydown", hold);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("pointermove", hold);
      window.removeEventListener("pointerdown", hold);
      window.removeEventListener("keydown", hold);
    };
  }, [overlayVisible]);

  const handleSurfaceClick = useCallback(
    (event: ReactMouseEvent<HTMLDivElement>) => {
      const target = event.target;
      if (
        target instanceof Element &&
        target.closest("button, a, input, [role='dialog']")
      ) {
        return;
      }
      // With the chrome up, a tap puts it away instead of turning a page —
      // the same rule the rest of the reader follows.
      if (overlayRef.current) {
        setOverlayVisible(false);
        return;
      }
      const rect = event.currentTarget.getBoundingClientRect();
      const share = (event.clientX - rect.left) / Math.max(1, rect.width);
      if (share < 0.3) turn(turnForSide("left", readingDirection));
      else if (share >= 0.7) turn(turnForSide("right", readingDirection));
      else setOverlayVisible(true);
    },
    [readingDirection, turn],
  );

  // Keys go through the same handle as the toolbar; grabfold's own keyboard
  // handling is off so a key cannot turn two books at once.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (isTypingTarget(event.target)) return;

      const turnKey = turnForKey(event.key, readingDirection, event.shiftKey);
      if (turnKey) {
        event.preventDefault();
        turn(turnKey);
        return;
      }

      switch (event.key) {
        case "Home":
          event.preventDefault();
          goToPosition(mapRef.current.first);
          break;
        case "End":
          event.preventDefault();
          goToPosition(mapRef.current.last);
          break;
        case "a":
        case "A":
          event.preventDefault();
          onToggleAutoPageTurn?.();
          break;
        case "f":
        case "F":
          event.preventDefault();
          onToggleFullscreen?.();
          break;
        case "o":
        case "O":
          event.preventDefault();
          setOverlayVisible((visible) => !visible);
          break;
        case "p":
        case "P":
          event.preventDefault();
          onTogglePageTurnMode?.();
          break;
        case "c":
        case "C":
          event.preventDefault();
          onToggleCover?.();
          break;
        default:
          break;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    goToPosition,
    readingDirection,
    turn,
    onToggleAutoPageTurn,
    onToggleFullscreen,
    onTogglePageTurnMode,
    onToggleCover,
  ]);

  // Auto page turn: stops at the end, like the paged reader does.
  useEffect(() => {
    if (!autoPageTurn) return;
    const interval = Math.max(1000, autoPageTurnIntervalMs);
    const timer = window.setInterval(() => {
      const book = bookRef.current;
      if (!book) return;
      if (book.book?.isTurning) return;
      if (!book.canNext()) {
        onToggleAutoPageTurn?.();
        return;
      }
      void book.next();
    }, interval);
    return () => window.clearInterval(timer);
  }, [autoPageTurn, autoPageTurnIntervalMs, onToggleAutoPageTurn]);

  /* ------------------------------------------------------------- render */

  const renderPage = (page: PageRef) => {
    const index = pageForRef(map, page);
    if (index === null) {
      return <div className="book-flip-blank" aria-hidden="true" />;
    }
    return (
      <BookPageImage
        key={index}
        index={index}
        images={images}
        onSize={handlePageSize}
      />
    );
  };

  return (
    <div
      className="reader-book relative h-full w-full overflow-hidden bg-kumo-base"
      style={{ paddingBottom: "env(safe-area-inset-bottom, 0px)" }}
      onClick={handleSurfaceClick}
    >
      <GrabfoldBook
        key={signature}
        ref={bookRef}
        sheets={map.sheets}
        covers={map.covers}
        binding={bookBinding(readingDirection)}
        layout={layout}
        pageRatio={pageRatio}
        fit="contain"
        position={view.position}
        onPositionChange={handlePositionChange}
        renderPage={renderPage}
        foldRenderer={foldRenderer ?? undefined}
        duration={620}
        commitAt={0.38}
        dragThreshold={8}
        flick
        peek
        grabBlock
        // No page-block edge beside the sheet: the book is the open page.
        thickness={0}
        shadow={0.32}
        gutter={0.24}
        lift={40}
        center
        // Keys and clicks are handled at the app level (the same paths the
        // toolbar uses), so the book must not also take the keyboard.
        keyboard={false}
        reducedMotion={reducedMotion}
        label="漫画"
        className="book-flip-host h-full w-full"
      />
    </div>
  );
});

/**
 * One comic page inside the book. Its image is loaded through the shared
 * cache, so the same decode also becomes the WebGL texture for the turn.
 * Keyed by page index: a slot that is reused for a different page starts over
 * instead of showing the previous page while the new one loads.
 */
function BookPageImage({
  index,
  images,
  onSize,
}: {
  index: number;
  images: PageImageCache;
  onSize: (ratio: number) => void;
}) {
  const [source, setSource] = useState<string | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    const loading = attempt > 0 ? images.retry(index) : images.load(index);
    loading
      .then((image) => {
        if (!live) return;
        const width = image.naturalWidth || 0;
        const height = image.naturalHeight || 0;
        if (width > 0 && height > 0) onSize(width / height);
        setSource(image.src);
        setState("ready");
      })
      .catch(() => {
        if (live) setState("error");
      });
    return () => {
      live = false;
    };
  }, [attempt, images, index, onSize]);

  return (
    <div className="book-flip-page">
      {state === "ready" && source ? (
        <img
          src={source}
          alt={`第 ${index + 1} 页`}
          draggable={false}
          decoding="async"
          className="book-flip-image"
        />
      ) : state === "error" ? (
        <div className="book-flip-page-state">
          <p>第 {index + 1} 页加载失败</p>
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              setState("loading");
              setAttempt((value) => value + 1);
            }}
            className="rounded-full border border-kumo-hairline bg-kumo-recessed px-3 py-1 text-xs text-kumo-default transition-colors hover:bg-kumo-tint"
          >
            重试
          </button>
        </div>
      ) : (
        <div className="book-flip-page-state" aria-hidden="true">
          <span className="book-flip-spinner" />
        </div>
      )}
    </div>
  );
}
