import type { ReactNode } from "react";
import { ArrowLeft } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { useBackNavigation } from "../../hooks/useBackNavigation";

interface PageHeaderProps {
  title: string;
  description?: string;
  icon?: ReactNode;
  actions?: ReactNode;
  back?: boolean;
  className?: string;
}

export function PageHeader({
  title,
  description,
  icon,
  actions,
  back,
  className,
}: PageHeaderProps) {
  const goBack = useBackNavigation("/");

  return (
    <div
      className={cn(
        "mb-5 flex flex-wrap items-center justify-between gap-3",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        {back && (
          <button
            type="button"
            onClick={goBack}
            aria-label="返回"
            className="flex size-8 shrink-0 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default"
          >
            <ArrowLeft className="size-4" weight="bold" />
          </button>
        )}
        {icon && (
          <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-[var(--app-accent-soft)] text-[var(--app-accent)]">
            {icon}
          </span>
        )}
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold tracking-tight">
            {title}
          </h1>
          {description && (
            <p className="mt-0.5 truncate text-sm text-kumo-subtle">
              {description}
            </p>
          )}
        </div>
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
