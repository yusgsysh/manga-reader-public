// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { ReaderAutoplayProgress } from "./ReaderAutoplayProgress";

describe("ReaderAutoplayProgress", () => {
  it("is absent while auto page turn is off", () => {
    const { container } = render(
      <ReaderAutoplayProgress
        enabled={false}
        overlayVisible={false}
        intervalMs={5000}
      />,
    );
    expect(container.querySelector(".reader-autoplay-progress")).toBeNull();
  });

  it("shows the bar once the overlay hides", () => {
    const { container } = render(
      <ReaderAutoplayProgress
        enabled
        overlayVisible={false}
        intervalMs={5000}
      />,
    );
    const bar = container.querySelector(
      ".reader-autoplay-progress",
    ) as HTMLElement;
    expect(bar.dataset.visible).toBe("true");
    const fill = container.querySelector(
      ".reader-autoplay-progress-bar",
    ) as HTMLElement;
    expect(fill.style.animationDuration).toBe("5000ms");
  });

  it("keeps the bar mounted but transparent while the chrome is up", () => {
    const { container } = render(
      <ReaderAutoplayProgress
        enabled
        overlayVisible
        intervalMs={5000}
      />,
    );
    const bar = container.querySelector(
      ".reader-autoplay-progress",
    ) as HTMLElement;
    expect(bar.dataset.visible).toBe("false");
  });
});
