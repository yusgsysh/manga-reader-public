import { createBrowserRouter } from "react-router";
import { AppLayout } from "../components/layout/AppLayout";
import { HomePage } from "../pages/HomePage";
import { SubscriptionsPage } from "../pages/SubscriptionsPage";
import { PopularPage } from "../pages/PopularPage";
import { SearchPage } from "../pages/SearchPage";
import { BookshelfPage } from "../pages/BookshelfPage";
import { GalleryDetailPage } from "../pages/GalleryDetailPage";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <AppLayout />,
    children: [
      { index: true, element: <HomePage /> },
      { path: "watched", element: <SubscriptionsPage /> },
      { path: "popular", element: <PopularPage /> },
      { path: "search", element: <SearchPage /> },
      { path: "bookshelf", element: <BookshelfPage /> },
      { path: "gallery/:id/:token", element: <GalleryDetailPage /> },
    ],
  },
]);
