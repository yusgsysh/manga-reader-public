// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ReaderControlsBar } from "./ReaderControlsBar";

const baseProps = {
  page: 2,
  total: 10,
  visible: true,
  readingDirection: "rtl" as const,
  canPrev: true,
  canNext: true,
  onTurnPage: () => {},
  onGoToPage: () => {},
};

describe("ReaderControlsBar", () => {
  it("shows the current page and total", () => {
    render(<ReaderControlsBar {...baseProps} />);
    expect((screen.getByLabelText("当前页码") as HTMLInputElement).value).toBe(
      "3",
    );
    expect(screen.getByText("/ 10")).toBeTruthy();
  });

  it("reports page turns from the shared bar", () => {
    const onTurnPage = vi.fn();
    render(<ReaderControlsBar {...baseProps} onTurnPage={onTurnPage} />);
    fireEvent.click(screen.getByLabelText("下一页"));
    fireEvent.click(screen.getByLabelText("上一页"));
    expect(onTurnPage.mock.calls).toEqual([["next"], ["previous"]]);
  });

  it("stays mounted while hidden so both modes keep one bar", () => {
    const { container } = render(
      <ReaderControlsBar {...baseProps} visible={false} />,
    );
    const bar = container.querySelector(".reader-controls-bar") as HTMLElement;
    expect(bar.dataset.visible).toBe("false");
    expect((screen.getByLabelText("当前页码") as HTMLInputElement).value).toBe(
      "3",
    );
  });

  it("jumps to a typed page (1-based input)", () => {
    const onGoToPage = vi.fn();
    render(<ReaderControlsBar {...baseProps} onGoToPage={onGoToPage} />);
    const input = screen.getByLabelText("当前页码") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "7" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onGoToPage).toHaveBeenCalledWith(6);
  });
});
