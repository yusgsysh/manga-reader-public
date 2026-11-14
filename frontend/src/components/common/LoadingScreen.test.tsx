// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { LoadingScreen, PageLoading } from "./LoadingScreen";

describe("LoadingScreen", () => {
  it("exposes a default accessible name for the spinner", () => {
    render(<LoadingScreen />);
    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "Loading",
    );
    expect(screen.queryByText("正在加载漫画…")).toBeNull();
  });

  it("labels the spinner and shows the message when given", () => {
    render(<LoadingScreen message="正在加载漫画…" />);
    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "正在加载漫画…",
    );
    expect(screen.getByText("正在加载漫画…")).toBeTruthy();
  });

  it("keeps the spinner still whether or not a message is shown", () => {
    const first = render(<LoadingScreen />);
    const plain = screen.getByRole("status");
    const plainClass = plain.getAttribute("class");
    const plainParentClass = plain.parentElement?.getAttribute("class");
    first.unmount();

    render(<LoadingScreen message="正在加载漫画…" />);
    const withMessage = screen.getByRole("status");
    // The spinner's own box and its container must not change when the message
    // appears; otherwise the circle visibly jumps as loading states swap.
    expect(withMessage.getAttribute("class")).toBe(plainClass);
    expect(withMessage.parentElement?.getAttribute("class")).toBe(
      plainParentClass,
    );
    // And the message must stay out of the layout flow.
    expect(screen.getByText("正在加载漫画…").className).toContain("absolute");
  });

  it("anchors the in-shell page loader to the viewport, not the content box", () => {
    const page = render(<PageLoading />);
    const spinner = screen.getByRole("status");
    // Fixed + inset-0 centers on the viewport regardless of the header or main
    // padding, so swapping LoadingScreen for PageLoading keeps the circle still.
    expect(spinner.parentElement?.className).toContain("fixed");
    expect(spinner.parentElement?.className).toContain("inset-0");
    // Same spinner box as LoadingScreen; a size change would read as a jump.
    const pageClass = spinner.getAttribute("class");
    page.unmount();

    render(<LoadingScreen />);
    expect(screen.getByRole("status").getAttribute("class")).toBe(pageClass);
  });
});
