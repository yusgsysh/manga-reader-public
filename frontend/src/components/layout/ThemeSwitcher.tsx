import { Monitor, Moon, Sun } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { useTheme } from "../../hooks/useTheme";
import type { ThemeMode } from "../../lib/theme";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

interface ThemeSwitcherProps {
  className?: string;
}

/** Segmented light / dark / system control, shared by the top bar and drawer. */
export function ThemeSwitcher({ className }: ThemeSwitcherProps) {
  const { mode, setMode } = useTheme();

  return (
    <div
      role="group"
      aria-label="主题模式"
      className={cn(
        "flex items-center gap-1 rounded-xl bg-kumo-recessed p-1",
        className,
      )}
    >
      {THEME_OPTIONS.map((option) => {
        const Icon = option.icon;
        const active = mode === option.mode;
        return (
          <button
            key={option.mode}
            type="button"
            onClick={() => setMode(option.mode)}
            aria-label={option.label}
            aria-pressed={active}
            title={option.label}
            className={cn(
              "flex flex-1 items-center justify-center rounded-lg py-1.5 transition-colors",
              active
                ? "bg-[var(--app-accent-soft)] text-[var(--app-accent)]"
                : "text-kumo-subtle hover:text-kumo-default",
            )}
          >
            <Icon className="size-4" weight={active ? "fill" : "regular"} />
          </button>
        );
      })}
    </div>
  );
}
