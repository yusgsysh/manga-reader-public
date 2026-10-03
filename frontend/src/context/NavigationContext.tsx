import { createContext, useContext, useState, type ReactNode } from "react";

interface NavigationContextValue {
  lastListRoute: string;
  setLastListRoute: (route: string) => void;
}

const NavigationContext = createContext<NavigationContextValue | null>(null);

export function NavigationProvider({ children }: { children: ReactNode }) {
  const [lastListRoute, setLastListRoute] = useState("/");

  return (
    <NavigationContext.Provider value={{ lastListRoute, setLastListRoute }}>
      {children}
    </NavigationContext.Provider>
  );
}

export function useNavigationContext(): NavigationContextValue {
  const context = useContext(NavigationContext);
  if (!context) {
    throw new Error("useNavigationContext must be used within a NavigationProvider");
  }
  return context;
}