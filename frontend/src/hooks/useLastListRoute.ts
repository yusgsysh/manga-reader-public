import { useEffect } from "react";
import { useNavigationContext } from "../context/NavigationContext";

export function useLastListRoute(route: string) {
  const { setLastListRoute } = useNavigationContext();

  useEffect(() => {
    setLastListRoute(route);
  }, [route, setLastListRoute]);
}