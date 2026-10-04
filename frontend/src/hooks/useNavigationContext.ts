import { useContext } from "react";
import { NavigationContext } from "../context/NavigationContextValue";
import type { NavigationContextValue } from "../context/types";

export function useNavigationContext(): NavigationContextValue {
  const context = useContext(NavigationContext);
  if (!context) {
    throw new Error("useNavigationContext must be used within a NavigationProvider");
  }
  return context;
}