import { useEffect } from "react";
import { useNavigationContext } from "./useNavigationContext";

export function useLastListRoute(route: string) {
  const { setLastListRoute } = useNavigationContext();

  useEffect(() => {
    setLastListRoute(route);
  }, [route, setLastListRoute]);
}