// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { ReaderPageList } from "./ReaderToolbarPanels";

describe("ReaderPageList", () => {
  it("renders one thumbnail per page", () => {
    const { container } = render(
      <ReaderPageList
        id={1}
        token="tok"
        total={3}
        currentPage={0}
        onGoToPage={() => {}}
      />,
    );
    expect(container.querySelectorAll(".reader-page-list-item")).toHaveLength(3);
  });

  it("renders the loading shimmer as a block element so it fills the tile", () => {
    // `.skeleton-shimmer` is `position: relative`, which does not blockify an
    // inline <span>. A <span> here would ignore `h-full w-full` and collapse
    // the placeholder to nothing (no visible "loading" state). The shimmer
    // must be a block-level <div>, matching the gallery grid.
    const { container } = render(
      <ReaderPageList
        id={1}
        token="tok"
        total={3}
        currentPage={0}
        onGoToPage={() => {}}
      />,
    );
    const shimmers = container.querySelectorAll(
      ".reader-page-list-item .skeleton-shimmer",
    );
    expect(shimmers).toHaveLength(3);
    for (const shimmer of shimmers) {
      expect(shimmer.tagName).toBe("DIV");
    }
  });
});
