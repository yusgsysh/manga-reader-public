import { describe, expect, it } from "vitest";
import {
  createThumbnailQueue,
  isPageThumbnailURL,
} from "./lazyThumbnails";

describe("isPageThumbnailURL", () => {
  it("matches the cached page thumbnail endpoint", () => {
    expect(
      isPageThumbnailURL(
        "http://localhost:8080/api/image-cache/page-thumbnail?url=x&x=0&y=0&w=4&h=4",
      ),
    ).toBe(true);
  });

  it("matches the index-based page thumbnail endpoint", () => {
    expect(
      isPageThumbnailURL(
        "http://localhost:8080/api/image/page-thumbnail?id=1&token=tok&index=0",
      ),
    ).toBe(true);
    expect(
      isPageThumbnailURL(
        "http://localhost:8080/api/image-cache/page-thumbnail?id=1&token=tok&index=0",
      ),
    ).toBe(true);
  });

  it("rejects unrelated image endpoints", () => {
    expect(
      isPageThumbnailURL("http://localhost:8080/api/image-cache/thumbnail?url=x"),
    ).toBe(false);
    expect(
      isPageThumbnailURL("http://localhost:8080/api/image-cache/page?url=x"),
    ).toBe(false);
    expect(isPageThumbnailURL("not a url")).toBe(false);
  });
});

interface FakeImage {
  src: string;
  fire: (type: "load" | "error") => void;
  addEventListener: (type: string, cb: () => void) => void;
  removeEventListener: (type: string, cb: () => void) => void;
}

function fakeImage(): HTMLImageElement & FakeImage {
  const listeners = new Map<string, Set<() => void>>();
  const img = {
    src: "",
    addEventListener(type: string, cb: () => void) {
      const set = listeners.get(type) ?? new Set();
      set.add(cb);
      listeners.set(type, set);
    },
    removeEventListener(type: string, cb: () => void) {
      listeners.get(type)?.delete(cb);
    },
    fire(type: string) {
      [...(listeners.get(type) ?? [])].forEach((cb) => cb());
    },
  };
  return img as unknown as HTMLImageElement & FakeImage;
}

describe("createThumbnailQueue", () => {
  it("limits how many images load at once and drains as they finish", () => {
    const queue = createThumbnailQueue(2);
    const imgs = Array.from({ length: 5 }, fakeImage);

    imgs.forEach((img, i) => queue.enqueue({ img, url: `u${i}` }));

    const started = () => imgs.filter((img) => img.src.startsWith("u")).length;
    expect(started()).toBe(2);

    imgs[0].fire("load");
    expect(started()).toBe(3);

    imgs[1].fire("error");
    expect(started()).toBe(4);

    imgs[2].fire("load");
    imgs[3].fire("load");
    expect(started()).toBe(5);

    queue.stop();
  });

  it("stops loading after stop()", () => {
    const queue = createThumbnailQueue(1);
    const first = fakeImage();
    const second = fakeImage();

    queue.enqueue({ img: first, url: "a" });
    expect(first.src).toBe("a");

    queue.stop();
    first.fire("load");

    queue.enqueue({ img: second, url: "b" });
    expect(second.src).toBe("");
  });
});
