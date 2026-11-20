// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ReaderToolbar } from "./ReaderToolbar";
import type { ReaderSettings } from "../../domain/reader/readerSettings";

const SETTINGS: ReaderSettings = {
  readingDirection: "rtl",
  pageTurnMode: "single",
  hasCover: true,
  autoPageTurnIntervalMs: 5000,
};

function renderToolbar(overrides: {
  overlayVisible?: boolean;
  viewerSettings?: ReaderSettings;
} = {}) {
  const callbacks = {
    onBack: vi.fn(),
    onToggleFullscreen: vi.fn(),
    onToggleAutoPageTurn: vi.fn(),
    onSetOverlayVisible: vi.fn(),
    onGoToPage: vi.fn(),
    onUpdateSettings: vi.fn(),
  };
  const view = render(
    <ReaderToolbar
      title="Gallery"
      fullscreen={false}
      overlayVisible={overrides.overlayVisible ?? false}
      viewerSettings={overrides.viewerSettings ?? SETTINGS}
      id={1}
      token="tok"
      total={3}
      getCurrentPage={() => 0}
      autoPageTurnEnabled={false}
      {...callbacks}
    />,
  );
  return { ...callbacks, ...view };
}

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

describe("ReaderToolbar keyboard map", () => {
  it("F toggles fullscreen and does not reach comimi's own handler", () => {
    const { onToggleFullscreen } = renderToolbar();
    const event = new KeyboardEvent("keydown", {
      key: "f",
      bubbles: true,
      cancelable: true,
    });
    document.body.dispatchEvent(event);

    expect(onToggleFullscreen).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);
  });

  it("C toggles between single and spread pages", () => {
    const first = renderToolbar();
    fireEvent.keyDown(document.body, { key: "c" });
    expect(first.onUpdateSettings).toHaveBeenCalledWith({
      pageTurnMode: "spread",
    });

    cleanup();

    const second = renderToolbar({
      viewerSettings: { ...SETTINGS, pageTurnMode: "spread" },
    });
    fireEvent.keyDown(document.body, { key: "C" });
    expect(second.onUpdateSettings).toHaveBeenCalledWith({
      pageTurnMode: "single",
    });
  });

  it("P opens and closes the page list panel", () => {
    renderToolbar();
    fireEvent.keyDown(document.body, { key: "p" });
    expect(screen.getByRole("dialog", { name: "页面列表" })).toBeTruthy();

    fireEvent.keyDown(document.body, { key: "p" });
    expect(screen.queryByRole("dialog", { name: "页面列表" })).toBeNull();
  });

  it("M reveals the menu when hidden and hides it when visible", () => {
    const hidden = renderToolbar({ overlayVisible: false });
    fireEvent.keyDown(document.body, { key: "m" });
    expect(hidden.onSetOverlayVisible).toHaveBeenCalledWith(true);

    cleanup();

    const visible = renderToolbar({ overlayVisible: true });
    fireEvent.keyDown(document.body, { key: "m" });
    expect(visible.onSetOverlayVisible).toHaveBeenCalledWith(false);
  });

  it("M closes an open panel with the menu", () => {
    const { onSetOverlayVisible } = renderToolbar();
    fireEvent.click(screen.getByRole("button", { name: "页面列表" }));
    expect(screen.getByRole("dialog", { name: "页面列表" })).toBeTruthy();

    fireEvent.keyDown(document.body, { key: "m" });
    expect(screen.queryByRole("dialog", { name: "页面列表" })).toBeNull();
    expect(onSetOverlayVisible).toHaveBeenCalledWith(false);
  });

  it("Escape closes the open panel first, then leaves the reader", () => {
    const { onBack } = renderToolbar();
    fireEvent.click(screen.getByRole("button", { name: "阅读设置" }));

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "阅读设置" })).toBeNull();
    expect(onBack).not.toHaveBeenCalled();

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("swallows the removed N and O bindings", () => {
    const { onToggleFullscreen } = renderToolbar();
    for (const key of ["n", "N", "o", "O"]) {
      const event = new KeyboardEvent("keydown", {
        key,
        bubbles: true,
        cancelable: true,
      });
      document.body.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
    }
    expect(onToggleFullscreen).not.toHaveBeenCalled();
  });

  it("leaves shortcuts alone while typing in a form control", () => {
    const { onUpdateSettings, onToggleFullscreen } = renderToolbar();
    const input = document.createElement("input");
    document.body.append(input);
    input.focus();

    fireEvent.keyDown(input, { key: "c" });
    fireEvent.keyDown(input, { key: "f" });

    expect(onUpdateSettings).not.toHaveBeenCalled();
    expect(onToggleFullscreen).not.toHaveBeenCalled();
    input.remove();
  });
});
