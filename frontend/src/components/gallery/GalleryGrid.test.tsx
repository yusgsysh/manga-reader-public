// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { GalleryGrid } from "./GalleryGrid";
import type { GalleryListItem } from "../../types/gallery";

function gallery(id: number): GalleryListItem {
  return {
    id,
    token: `token-${id}`,
    title: `Title ${id}`,
    category: "doujinshi",
    cover: `cover-${id}`,
    posted: "2026-01-01 00:00",
    rating: 4.5,
    url: `https://example.test/${id}`,
    uploader: "uploader",
    pages: 20,
    domain: "exhentai",
  };
}

function renderGrid(
  galleries: GalleryListItem[],
  loadingMore?: boolean,
) {
  return render(
    <MemoryRouter>
      <GalleryGrid galleries={galleries} loadingMore={loadingMore} />
    </MemoryRouter>,
  );
}

describe("GalleryGrid", () => {
  it("renders loaded cards without any entrance animation", () => {
    const { container } = renderGrid([gallery(1), gallery(2), gallery(3)]);

    expect(container.querySelectorAll(".gallery-card")).toHaveLength(3);
    expect(container.querySelectorAll(".gallery-skeleton")).toHaveLength(0);
  });

  it("holds the next page's row with skeleton cards while it loads", () => {
    // jsdom's 1024px viewport maps to 5 grid columns.
    const { container } = renderGrid([gallery(1)], true);

    expect(container.querySelectorAll(".gallery-card")).toHaveLength(1);
    expect(container.querySelectorAll(".gallery-skeleton")).toHaveLength(5);
  });
});
