import type { ReactNode } from "react";
import { m } from "motion/react";

interface PageTransitionProps {
  routeKey: string;
  children: ReactNode;
}

export function PageTransition({ routeKey, children }: PageTransitionProps) {
  return (
    <m.div
      key={routeKey}
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.18, ease: "easeOut" }}
    >
      {children}
    </m.div>
  );
}
