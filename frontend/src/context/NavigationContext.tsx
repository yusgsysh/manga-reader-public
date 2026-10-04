import { useState, type ReactNode } from "react";
import { NavigationContext } from "./NavigationContextValue";

export function NavigationProvider({ children }: { children: ReactNode }) {
  const [lastListRoute, setLastListRoute] = useState("/");

  return (
    <NavigationContext.Provider value={{ lastListRoute, setLastListRoute }}>
      {children}
    </NavigationContext.Provider>
  );
}