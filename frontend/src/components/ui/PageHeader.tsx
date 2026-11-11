import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  actions?: ReactNode;
}

/*
 * The top bar already names the current page, so a visible title + icon row
 * only repeated the active destination. Keep the heading for document
 * structure and screen readers, and leave just the page's actions on screen.
 */
export function PageHeader({ title, actions }: PageHeaderProps) {
  if (!actions) return <h1 className="sr-only">{title}</h1>;

  return (
    <div className="mb-5 flex flex-wrap items-center justify-end gap-3">
      <h1 className="sr-only">{title}</h1>
      <div className="flex shrink-0 items-center gap-2">{actions}</div>
    </div>
  );
}
