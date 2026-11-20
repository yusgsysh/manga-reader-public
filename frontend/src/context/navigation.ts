import { createContext, useContext } from "react";

export interface NavigationContextValue {
  lastListRoute: string;
  setLastListRoute: (route: string) => void;
}

export const NavigationContext =
  createContext<NavigationContextValue | null>(null);

export function useNavigationContext(): NavigationContextValue {
  const context = useContext(NavigationContext);
  if (!context) {
    throw new Error(
      "useNavigationContext must be used within a NavigationProvider",
    );
  }
  return context;
}
