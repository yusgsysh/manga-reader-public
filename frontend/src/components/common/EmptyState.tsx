import { Button } from "@cloudflare/kumo";
import { useNavigate } from "react-router";
import { FolderOpen } from "@phosphor-icons/react";

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
    <div className="flex flex-col items-center justify-center gap-4 py-16 text-center">
      <span className="flex size-12 items-center justify-center rounded-2xl bg-kumo-recessed text-kumo-inactive">
        <FolderOpen className="size-6" />
      </span>
      <p className="text-sm text-kumo-subtle">{message ?? "暂无数据"}</p>
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
