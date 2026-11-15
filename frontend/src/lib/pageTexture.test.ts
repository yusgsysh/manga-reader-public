// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fitPageTexture, pageFit } from "./pageTexture";

function image(width: number, height: number): HTMLImageElement {
  return {
    naturalWidth: width,
    naturalHeight: height,
    width,
    height,
  } as HTMLImageElement;
}

function stubCanvas() {
  const drawImage = vi.fn();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    drawImage,
    imageSmoothingEnabled: false,
    imageSmoothingQuality: "low",
  } as unknown as CanvasRenderingContext2D);
  return drawImage;
}

describe("fitPageTexture", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the image when it already matches the page cell", () => {
    const source = image(700, 1000);
    expect(fitPageTexture(source, 0.7)).toBe(source);
  });

  it("contains a wide image inside a portrait page cell", () => {
    const drawImage = stubCanvas();
    const source = image(1200, 600);
    const result = fitPageTexture(source, 0.7);
    expect(result).toBeInstanceOf(HTMLCanvasElement);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(1200);
    expect(canvas.height).toBe(1714);
    expect(drawImage).toHaveBeenCalledWith(
      source,
      0,
      (1714 - 600) / 2,
      1200,
      600,
    );
  });

  it("contains a tall image inside a landscape page cell", () => {
    const drawImage = stubCanvas();
    const source = image(800, 2000);
    const result = fitPageTexture(source, 1.4);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(800);
    expect(canvas.height).toBe(571);
    const [drawSource, x, y, width, height] = drawImage.mock.calls[0];
    expect(drawSource).toBe(source);
    expect(width).toBeCloseTo(571 * (800 / 2000), 1);
    expect(height).toBeCloseTo(571, 1);
    expect(x).toBeCloseTo((800 - width) / 2, 1);
    expect(y).toBeCloseTo(0, 1);
  });

  it("fills near-matching pages to remove the paper rim", () => {
    expect(pageFit(704 / 1000, 0.7)).toBe("cover");
    expect(pageFit(1200 / 600, 0.7)).toBe("contain");

    const drawImage = stubCanvas();
    const source = image(704, 1000);
    const result = fitPageTexture(source, 0.7);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(704);
    expect(canvas.height).toBe(1006);
    const [, x, y, width, height] = drawImage.mock.calls[0];
    expect(width).toBeCloseTo(708.2, 1);
    expect(height).toBeCloseTo(1006, 1);
    expect(x).toBeLessThan(0);
    expect(y).toBeCloseTo(0, 1);
  });

  it("caps huge textures while keeping the page ratio", () => {
    stubCanvas();
    const source = image(3000, 1000);
    const result = fitPageTexture(source, 0.7);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.height).toBe(2048);
    expect(canvas.width).toBe(Math.round(2048 * 0.7));
  });
});
