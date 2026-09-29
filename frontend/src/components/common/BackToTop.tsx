import { useEffect, useState } from "react";
import { RocketLaunch } from "@phosphor-icons/react";

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
      onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
      style={{ right: "max(1.5rem, calc((100vw - 80rem) / 2 + 1.5rem))" }}
      className={`fixed bottom-6 z-50 flex size-11 items-center justify-center rounded-full border border-kumo-border bg-kumo-elevated text-kumo-default shadow-lg transition-all duration-200 hover:bg-kumo-subtle active:scale-95 md:bottom-8 ${
        visible
          ? "translate-y-0 opacity-100"
          : "pointer-events-none translate-y-2 opacity-0"
      }`}
    >
      <RocketLaunch size={22} weight="duotone" />
    </button>
  );
}
