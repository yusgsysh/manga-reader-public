import type { ReactNode } from "react";

interface ListingLayoutProps {
  children: ReactNode;
  sidebar?: ReactNode;
}

/**
 * Two-column listing shell: main content plus a compact side rail.
 *
 * On mobile the sidebar is rendered first (above the list) and stacks to full
 * width; on desktop it becomes a sticky right-hand rail.
 */
export function ListingLayout({ children, sidebar }: ListingLayoutProps) {
  return (
    <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:gap-8">
      {sidebar && (
        <aside className="w-full lg:order-last lg:sticky lg:top-6 lg:w-64 lg:shrink-0">
          {sidebar}
        </aside>
      )}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
