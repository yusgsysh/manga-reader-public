// @vitest-environment jsdom
import { act, render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("GalleryGrid", () => {
  it("streams cards in one at a time, reserving their space up front", () => {
    const { container } = render(
      <MemoryRouter>
        <GalleryGrid galleries={[gallery(1), gallery(2), gallery(3)]} />
      </MemoryRouter>,
    );

    // Only the first card is mounted; the rest hold their footprint so the
    // page does not jump (and the scroll sentinel stays at the real bottom).
    expect(container.querySelectorAll(".gallery-card")).toHaveLength(1);
    expect(container.querySelectorAll(".gallery-slot")).toHaveLength(2);

    act(() => {
      vi.advanceTimersByTime(30);
    });
    expect(container.querySelectorAll(".gallery-card")).toHaveLength(2);
    expect(container.querySelectorAll(".gallery-slot")).toHaveLength(1);

    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(container.querySelectorAll(".gallery-card")).toHaveLength(3);
    expect(container.querySelectorAll(".gallery-slot")).toHaveLength(0);
  });
});
