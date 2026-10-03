import type { ReactNode } from "react";

interface PageTransitionProps {
  routeKey: string;
  children: ReactNode;
}

/*
 * Pure CSS keyframes instead of motion: the route change already mounts the
 * new page's content (grid, images) on the main thread, and driving this
 * animation from JS as well is what makes the transition stutter on low-end
 * devices. A CSS transform/opacity animation runs entirely on the compositor,
 * and without a fill mode the element keeps no transform once it finishes
 * (so no permanent full-page composited layer).
 */
export function PageTransition({ routeKey, children }: PageTransitionProps) {
  return (
    <div key={routeKey} className="page-enter">
      {children}
    </div>
  );
}
