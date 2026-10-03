import { useEffect } from "react";

export function useDocumentTitle(pageTitle?: string) {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = pageTitle ?? "Manga Reader";

    return () => {
      document.title = previousTitle;
    };
  }, [pageTitle]);
}