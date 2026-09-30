import { useEffect, useState } from "react";
import { ArrowUp } from "@phosphor-icons/react";

const SHOW_AFTER = 400;

export function BackToTop() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    let frame = 0;
    const onScroll = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        setVisible(window.scrollY > SHOW_AFTER);
      });
    };

    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <button
      type="button"
      aria-label="回到顶端"
      title="回到顶端"
      onClick={() =>
        window.scrollTo({
          top: 0,
          behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches
            ? "auto"
            : "smooth",
        })
      }
      style={{
        right: "max(1.5rem, calc((100vw - var(--app-max-width)) / 2 + 1.5rem))",
      }}
      className={`fixed bottom-[calc(1.5rem+env(safe-area-inset-bottom))] z-50 flex size-11 items-center justify-center rounded-full border border-kumo-hairline bg-kumo-elevated text-kumo-default shadow-lg transition-all duration-200 hover:bg-kumo-tint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)] active:scale-95 md:bottom-[calc(2rem+env(safe-area-inset-bottom))] ${
        visible
          ? "translate-y-0 opacity-100"
          : "pointer-events-none translate-y-2 opacity-0"
      }`}
    >
      <ArrowUp size={20} weight="bold" />
    </button>
  );
}
