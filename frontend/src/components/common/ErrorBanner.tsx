import type { ReactNode } from "react";
import { Button } from "@cloudflare/kumo";
import { ArrowsClockwise, WarningCircle } from "@phosphor-icons/react";

interface ErrorBannerProps {
  message?: ReactNode;
  onRetry?: () => void;
  retryLabel?: string;
  className?: string;
}

/**
 * Inline error banner shared across the app: a compact, icon-led alert box
 * with an optional retry action. Every inline error surface (login, dialogs,
 * refresh failures, …) renders through here so the styling stays consistent.
 */
export function ErrorBanner({
  message,
  onRetry,
  retryLabel = "重试",
  className,
}: ErrorBannerProps) {
  return (
    <div
      role="alert"
      className={[
        "error-banner flex items-start gap-2.5 rounded-xl border border-kumo-danger/25 bg-kumo-danger/10 px-3.5 py-2.5 text-sm leading-5 text-kumo-danger",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <WarningCircle
        className="mt-0.5 size-4 shrink-0"
        weight="fill"
        aria-hidden
      />
      <span className="min-w-0 flex-1">{message ?? "发生错误"}</span>
      {onRetry && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onRetry}
          className="-my-1 shrink-0"
        >
          <ArrowsClockwise className="mr-1 size-3.5" weight="bold" />
          {retryLabel}
        </Button>
      )}
    </div>
  );
}
