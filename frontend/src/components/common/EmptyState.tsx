import { Button } from "@cloudflare/kumo";
import { useNavigate } from "react-router";

interface EmptyStateProps {
  message?: string;
  actionLabel?: string;
  actionTo?: string;
}

export function EmptyState({
  message,
  actionLabel,
  actionTo,
}: EmptyStateProps) {
  const navigate = useNavigate();

  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16">
      <p className="text-kumo-subtle text-sm">
        {message ?? "暂无数据"}
      </p>
      {actionLabel && actionTo && (
        <Button
          onClick={() => navigate(actionTo)}
          variant="secondary"
          size="sm"
        >
          {actionLabel}
        </Button>
      )}
    </div>
  );
}
