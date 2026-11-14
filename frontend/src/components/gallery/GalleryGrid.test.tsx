// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { GalleryGrid } from "./GalleryGrid";
import { GalleryGridSkeleton } from "./GallerySkeleton";
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

function renderGrid(galleries: GalleryListItem[]) {
  return render(
    <MemoryRouter>
      <GalleryGrid galleries={galleries} />
    </MemoryRouter>,
  );
}

describe("GalleryGrid", () => {
  it("renders an appended page immediately, without placeholder cells", () => {
    const { container, rerender } = renderGrid([gallery(1), gallery(2)]);

    expect(container.querySelectorAll(".gallery-card")).toHaveLength(2);

    rerender(
      <MemoryRouter>
        <GalleryGrid
          galleries={[gallery(1), gallery(2), gallery(3), gallery(4)]}
        />
      </MemoryRouter>,
    );

    // Cards land at once and the grid holds exactly the real cards: no extra
    // placeholder can extend a partial row and collapse again.
    expect(container.querySelectorAll(".gallery-card")).toHaveLength(4);
    expect(container.querySelector(".gallery-grid")?.children).toHaveLength(4);
  });
});

describe("GalleryGridSkeleton", () => {
  it("keeps the shimmering loading frames", () => {
    const { container } = render(<GalleryGridSkeleton count={3} />);

    expect(container.querySelector(".gallery-grid")?.children).toHaveLength(3);
    // Every skeleton block animates while the page is loading.
    expect(
      container.querySelectorAll(".skeleton-shimmer").length,
    ).toBeGreaterThanOrEqual(3);
  });
});
