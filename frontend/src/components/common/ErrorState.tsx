import { Button } from "@cloudflare/kumo";
import { ArrowLeft } from "lucide-react";

interface ErrorStateProps {
  message?: string;
  onRetry?: () => void;
}

export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16">
      <p className="text-kumo-subtle text-sm">
        {message ?? "无法加载数据"}
      </p>
      {onRetry && (
        <Button onClick={onRetry} variant="secondary" size="sm">
          <ArrowLeft className="mr-1 size-3" />
          重新加载
        </Button>
      )}
    </div>
  );
}
