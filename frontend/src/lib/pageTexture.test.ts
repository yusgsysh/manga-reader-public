// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fitPageTexture } from "./pageTexture";

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
    fillRect: vi.fn(),
    set fillStyle(_value: string) {},
    imageSmoothingEnabled: false,
    imageSmoothingQuality: "low",
  } as unknown as CanvasRenderingContext2D);
  return drawImage;
}

/** The last draw is the sharp page; earlier ones build the ambient bleed. */
function lastDraw(drawImage: ReturnType<typeof stubCanvas>) {
  return drawImage.mock.calls[drawImage.mock.calls.length - 1] as [
    CanvasImageSource,
    number,
    number,
    number,
    number,
  ];
}

describe("fitPageTexture", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns the image when it already matches the page cell", () => {
    const source = image(700, 1000);
    expect(fitPageTexture(source, 0.7)).toBe(source);
  });

  it("contains a wide page over a blurred copy of itself", () => {
    const drawImage = stubCanvas();
    const source = image(1200, 600);
    const result = fitPageTexture(source, 0.7);
    expect(result).toBeInstanceOf(HTMLCanvasElement);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(1200);
    expect(canvas.height).toBe(1714);
    expect(drawImage.mock.calls.length).toBeGreaterThanOrEqual(3);
    const [drawSource, x, y, width, height] = lastDraw(drawImage);
    expect(drawSource).toBe(source);
    expect(x).toBe(0);
    expect(y).toBe((1714 - 600) / 2);
    expect(width).toBe(1200);
    expect(height).toBe(600);
  });

  it("contains a tall page over a blurred copy of itself", () => {
    const drawImage = stubCanvas();
    const source = image(800, 2000);
    const result = fitPageTexture(source, 1.4);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(800);
    expect(canvas.height).toBe(571);
    const [, x, y, width, height] = lastDraw(drawImage);
    expect(width).toBeCloseTo(571 * (800 / 2000), 1);
    expect(height).toBeCloseTo(571, 1);
    expect(x).toBeCloseTo((800 - width) / 2, 1);
    expect(y).toBeCloseTo(0, 1);
  });

  it("keeps a near-matching page uncropped over its bleed", () => {
    const drawImage = stubCanvas();
    const source = image(704, 1000);
    const result = fitPageTexture(source, 0.7);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.width).toBe(704);
    expect(canvas.height).toBe(1006);
    const [drawSource, x, y, width, height] = lastDraw(drawImage);
    expect(drawSource).toBe(source);
    expect(x).toBe(0);
    expect(y).toBeCloseTo((1006 - 1000) / 2, 1);
    expect(width).toBe(704);
    expect(height).toBe(1000);
  });

  it("caps huge textures while keeping the page ratio", () => {
    const drawImage = stubCanvas();
    const source = image(3000, 1000);
    const result = fitPageTexture(source, 0.7);
    const canvas = result as HTMLCanvasElement;
    expect(canvas.height).toBe(2048);
    expect(canvas.width).toBe(1434);
    const [, x, y, width, height] = lastDraw(drawImage);
    expect(width).toBe(1434);
    expect(height).toBeCloseTo(478, 0);
    expect(x).toBe(0);
    expect(y).toBeCloseTo((2048 - 478) / 2, 0);
  });
});
