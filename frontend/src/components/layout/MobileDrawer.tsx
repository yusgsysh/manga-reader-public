import { useEffect, useRef } from "react";
import { Link, useLocation } from "react-router";
import { AnimatePresence, m } from "motion/react";
import { X } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { Logo } from "../ui";
import { PRIMARY_NAV, SECONDARY_NAV, isNavActive, type NavItem } from "./nav";

interface MobileDrawerProps {
  open: boolean;
  onClose: () => void;
}

function DrawerLink({
  item,
  active,
  onNavigate,
}: {
  item: NavItem;
  active: boolean;
  onNavigate: () => void;
}) {
  const Icon = item.icon;
  return (
    <Link
      to={item.to}
      onClick={onNavigate}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-colors",
        active
          ? "bg-kumo-contrast text-kumo-inverse"
          : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
      )}
    >
      <Icon className="size-5 shrink-0" weight={active ? "fill" : "regular"} />
      {item.label}
    </Link>
  );
}

export function MobileDrawer({ open, onClose }: MobileDrawerProps) {
  const location = useLocation();
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    panelRef.current?.focus();
    return () => {
      document.body.style.overflow = previous;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open, onClose]);

  return (
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[60] md:hidden">
          <m.div
            className="absolute inset-0 bg-black/40 backdrop-blur-[2px]"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onClick={onClose}
          />
          <m.div
            ref={panelRef}
            role="dialog"
            aria-modal="true"
            aria-label="导航菜单"
            tabIndex={-1}
            className="absolute left-0 top-0 flex h-full w-[78vw] max-w-xs flex-col bg-kumo-elevated pt-[env(safe-area-inset-top)] shadow-2xl outline-none"
            initial={{ x: "-100%" }}
            animate={{ x: 0 }}
            exit={{ x: "-100%" }}
            transition={{ type: "tween", duration: 0.22, ease: "easeOut" }}
          >
            <div className="flex h-14 items-center justify-between px-4">
              <Logo />
              <button
                type="button"
                onClick={onClose}
                aria-label="关闭菜单"
                className="flex size-9 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default"
              >
                <X className="size-5" weight="bold" />
              </button>
            </div>

            <nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 pb-[env(safe-area-inset-bottom)]">
              {PRIMARY_NAV.map((item) => (
                <DrawerLink
                  key={item.to}
                  item={item}
                  active={isNavActive(location.pathname, item.to)}
                  onNavigate={onClose}
                />
              ))}

              <div className="my-3 border-t border-kumo-hairline" />

              {SECONDARY_NAV.map((item) => (
                <DrawerLink
                  key={item.to}
                  item={item}
                  active={isNavActive(location.pathname, item.to)}
                  onNavigate={onClose}
                />
              ))}
            </nav>
          </m.div>
        </div>
      )}
    </AnimatePresence>
  );
}
