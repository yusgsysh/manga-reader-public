// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { PageSkeleton } from "./PageSkeleton";

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PageSkeleton />
    </MemoryRouter>,
  );
}

describe("PageSkeleton", () => {
  it("paints the home listing's action row above a gallery grid", () => {
    const { container } = renderAt("/");
    // AdvancedSearchMenu + JumpSeekMenu are size-8 icon buttons.
    expect(container.querySelectorAll(".size-8")).toHaveLength(2);
    expect(container.querySelector(".gallery-grid")).toBeTruthy();
  });

  it("does not invent header actions on a listing route without them", () => {
    const { container } = renderAt("/popular");
    expect(container.querySelectorAll(".size-8")).toHaveLength(0);
    expect(container.querySelector(".gallery-grid")).toBeTruthy();
  });

  it("keeps the search field and filter card so search does not jump", () => {
    const { container } = renderAt("/search");
    expect(container.querySelector(".h-9.flex-1")).toBeTruthy();
    expect(container.querySelector(".gallery-grid")).toBeTruthy();
  });

  it("falls back to blocks for routes that are neither listings nor search", () => {
    const { container } = renderAt("/settings");
    expect(container.querySelector(".gallery-grid")).toBeNull();
    expect(container.querySelector(".h-24")).toBeTruthy();
  });
});
