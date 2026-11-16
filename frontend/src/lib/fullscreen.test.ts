// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  canRequestFullscreen,
  exitElementFullscreen,
  getFullscreenElement,
  requestElementFullscreen,
} from "./fullscreen";

afterEach(() => {
  vi.restoreAllMocks();
  Reflect.deleteProperty(document, "fullscreenElement");
  Reflect.deleteProperty(document, "webkitFullscreenElement");
  Reflect.deleteProperty(document, "exitFullscreen");
  Reflect.deleteProperty(document, "webkitExitFullscreen");
});

describe("getFullscreenElement", () => {
  it("reads the standard fullscreen element", () => {
    const element = document.createElement("div");
    Object.defineProperty(document, "fullscreenElement", {
      configurable: true,
      value: element,
    });
    expect(getFullscreenElement()).toBe(element);
  });

  it("falls back to the webkit-prefixed element", () => {
    const element = document.createElement("div");
    Object.defineProperty(document, "webkitFullscreenElement", {
      configurable: true,
      value: element,
    });
    expect(getFullscreenElement()).toBe(element);
  });

  it("returns null when nothing is fullscreen", () => {
    expect(getFullscreenElement()).toBeNull();
  });
});

describe("requestElementFullscreen", () => {
  it("uses the standard API with navigation UI hidden", async () => {
    const element = document.createElement("div");
    const request = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(element, "requestFullscreen", {
      configurable: true,
      value: request,
    });

    await expect(requestElementFullscreen(element)).resolves.toBe(true);
    expect(request).toHaveBeenCalledWith({ navigationUI: "hide" });
  });

  it("falls back to webkitRequestFullscreen", async () => {
    const element = document.createElement("div");
    const request = vi.fn();
    Object.defineProperty(element, "webkitRequestFullscreen", {
      configurable: true,
      value: request,
    });

    await expect(requestElementFullscreen(element)).resolves.toBe(true);
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("reports false when the browser has no fullscreen entry", async () => {
    const element = document.createElement("div");
    expect(canRequestFullscreen(element)).toBe(false);
    await expect(requestElementFullscreen(element)).resolves.toBe(false);
  });

  it("reports false instead of throwing when the request is rejected", async () => {
    const element = document.createElement("div");
    Object.defineProperty(element, "requestFullscreen", {
      configurable: true,
      value: vi.fn().mockRejectedValue(new TypeError("denied")),
    });

    await expect(requestElementFullscreen(element)).resolves.toBe(false);
  });
});

describe("exitElementFullscreen", () => {
  it("exits through the standard API when fullscreen is active", async () => {
    const exit = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(document, "fullscreenElement", {
      configurable: true,
      value: document.createElement("div"),
    });
    Object.defineProperty(document, "exitFullscreen", {
      configurable: true,
      value: exit,
    });

    await exitElementFullscreen();
    expect(exit).toHaveBeenCalledTimes(1);
  });

  it("does nothing when not in fullscreen", async () => {
    const exit = vi.fn();
    Object.defineProperty(document, "exitFullscreen", {
      configurable: true,
      value: exit,
    });

    await exitElementFullscreen();
    expect(exit).not.toHaveBeenCalled();
  });
});
