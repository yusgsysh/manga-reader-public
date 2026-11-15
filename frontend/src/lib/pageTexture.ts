/** Largest texture edge kept when composing a page for the GPU. */
const MAX_TEXTURE = 2048;

/** Width of the tiny image used to build the blurred page background. */
const BLEED_WIDTH = 64;

/**
 * WebGL draws a page image straight over the sheet, so an image whose aspect
 * ratio differs from the book's page cell would stretch. Compose it into a
 * canvas with the cell's ratio instead: a heavily blurred, cover-cropped copy
 * of the same image fills the margins (ambient bleed), and the sharp page is
 * drawn contained on top. Nothing is cropped from the page itself, and no
 * paper margin is left to show as a white rim while peeking or turning.
 */
export function fitPageTexture(
  image: HTMLImageElement,
  pageRatio: number,
): HTMLImageElement | HTMLCanvasElement {
  const width = image.naturalWidth || image.width;
  const height = image.naturalHeight || image.height;
  if (!width || !height || !Number.isFinite(pageRatio) || pageRatio <= 0) {
    return image;
  }
  // Already the page's shape: the GPU can sample it directly.
  if (Math.abs(width / height / pageRatio - 1) < 0.0005) return image;

  let canvasWidth = width;
  let canvasHeight = Math.round(width / pageRatio);
  const longEdge = Math.max(canvasWidth, canvasHeight);
  if (longEdge > MAX_TEXTURE) {
    const scale = MAX_TEXTURE / longEdge;
    canvasWidth = Math.round(canvasWidth * scale);
    canvasHeight = Math.round(canvasHeight * scale);
  }

  const canvas = document.createElement("canvas");
  canvas.width = canvasWidth;
  canvas.height = canvasHeight;
  const ctx = canvas.getContext("2d");
  if (!ctx) return image;
  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = "high";

  // The bleed: downscale the image with a cover fit, then stretch that tiny
  // canvas back over the page. The bilinear upscale is a cheap, smooth blur.
  const blurWidth = BLEED_WIDTH;
  const blurHeight = Math.max(1, Math.round(blurWidth / pageRatio));
  const blur = document.createElement("canvas");
  blur.width = blurWidth;
  blur.height = blurHeight;
  const blurCtx = blur.getContext("2d");
  if (blurCtx) {
    blurCtx.imageSmoothingEnabled = true;
    blurCtx.imageSmoothingQuality = "high";
    const coverScale = Math.max(blurWidth / width, blurHeight / height);
    const coverWidth = width * coverScale;
    const coverHeight = height * coverScale;
    blurCtx.drawImage(
      image,
      (blurWidth - coverWidth) / 2,
      (blurHeight - coverHeight) / 2,
      coverWidth,
      coverHeight,
    );
    ctx.drawImage(blur, 0, 0, canvasWidth, canvasHeight);
    ctx.fillStyle = "rgba(0, 0, 0, 0.28)";
    ctx.fillRect(0, 0, canvasWidth, canvasHeight);
  }

  // The page itself: contained, never cropped or stretched.
  const scale = Math.min(canvasWidth / width, canvasHeight / height);
  const drawWidth = width * scale;
  const drawHeight = height * scale;
  ctx.drawImage(
    image,
    (canvasWidth - drawWidth) / 2,
    (canvasHeight - drawHeight) / 2,
    drawWidth,
    drawHeight,
  );
  return canvas;
}
