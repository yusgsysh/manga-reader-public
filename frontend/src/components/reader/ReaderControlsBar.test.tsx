// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { ReaderControlsBar } from "./ReaderControlsBar";

const baseProps = {
  page: 2,
  total: 10,
  visible: true,
  readingDirection: "rtl" as const,
  onGoToPage: () => {},
};

describe("ReaderControlsBar", () => {
  it("shows the current page and total like comimi's seek bar", () => {
    const { container } = render(<ReaderControlsBar {...baseProps} />);
    expect(
      container.querySelector(".comimi-seek-current")?.textContent,
    ).toBe("3");
    expect(container.querySelector(".comimi-seek-total")?.textContent).toBe(
      " / 10",
    );
    const fill = container.querySelector(".comimi-seek-fill") as HTMLElement;
    expect(parseFloat(fill.style.width)).toBeCloseTo((2 / 9) * 100, 2);
  });

  it("jumps to the scrubbed page on release", () => {
    const onGoToPage = vi.fn();
    const { container } = render(
      <ReaderControlsBar {...baseProps} onGoToPage={onGoToPage} />,
    );
    const input = container.querySelector(
      ".comimi-seek-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "7" } });
    fireEvent.pointerUp(input);
    expect(onGoToPage).toHaveBeenCalledWith(7);
  });

  it("toggles comimi's overlay state and leaves the tab order when hidden", () => {
    const { container, rerender } = render(
      <ReaderControlsBar {...baseProps} visible={false} />,
    );
    const dock = container.querySelector(
      ".reader-book-dock",
    ) as HTMLElement;
    expect(dock.dataset.overlay).toBe("false");
    expect(dock.hasAttribute("inert")).toBe(true);

    rerender(<ReaderControlsBar {...baseProps} visible />);
    expect(dock.dataset.overlay).toBe("true");
    expect(dock.hasAttribute("inert")).toBe(false);
  });
});
