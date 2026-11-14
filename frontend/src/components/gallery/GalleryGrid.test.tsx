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

function renderGrid(galleries: GalleryListItem[], loadingMore?: boolean) {
  return render(
    <MemoryRouter>
      <GalleryGrid galleries={galleries} loadingMore={loadingMore} />
    </MemoryRouter>,
  );
}

describe("GalleryGrid", () => {
  it("renders an appended page immediately, without an entrance stagger", () => {
    const { container, rerender } = renderGrid([gallery(1), gallery(2)]);

    expect(container.querySelectorAll(".gallery-card")).toHaveLength(2);

    rerender(
      <MemoryRouter>
        <GalleryGrid
          galleries={[gallery(1), gallery(2), gallery(3), gallery(4)]}
        />
      </MemoryRouter>,
    );

    expect(container.querySelectorAll(".gallery-card")).toHaveLength(4);
    expect(container.querySelector(".gallery-grid")?.children).toHaveLength(4);
  });

  it("shows one skeleton row while the next page loads, on a fresh row", () => {
    // jsdom's 1024px viewport maps to 5 grid columns.
    const { container } = renderGrid([gallery(1), gallery(2)], true);

    const cells = container.querySelector(".gallery-grid")!.children;
    expect(cells).toHaveLength(2 + 5);
    // The first placeholder is pinned to column 1, so it never fills the empty
    // cells of the last real row (the "one extra card" bug).
    expect(cells[2].className).toContain("col-start-1");
    expect(
      container.querySelectorAll(".skeleton-shimmer").length,
    ).toBeGreaterThanOrEqual(5);
  });
});

describe("GalleryGridSkeleton", () => {
  it("keeps the shimmering loading frames", () => {
    const { container } = render(<GalleryGridSkeleton count={3} />);

    expect(container.querySelector(".gallery-grid")?.children).toHaveLength(3);
    expect(
      container.querySelectorAll(".skeleton-shimmer").length,
    ).toBeGreaterThanOrEqual(3);
  });
});
