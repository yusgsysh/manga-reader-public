import { Button } from "@cloudflare/kumo";
import { ArrowsClockwise, WarningCircle } from "@phosphor-icons/react";

interface ErrorStateProps {
  message?: string;
  onRetry?: () => void;
}

export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16 text-center">
      <span className="flex size-12 items-center justify-center rounded-2xl bg-kumo-danger/10 text-kumo-danger">
        <WarningCircle className="size-6" weight="fill" />
      </span>
      <p className="text-sm text-kumo-subtle">{message ?? "无法加载数据"}</p>
      {onRetry && (
        <Button onClick={onRetry} variant="secondary" size="sm">
          <ArrowsClockwise className="mr-1 size-3.5" weight="bold" />
          重新加载
        </Button>
      )}
    </div>
  );
}
