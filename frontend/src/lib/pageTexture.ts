/** Largest texture edge kept when letterboxing a page for the GPU. */
const MAX_TEXTURE = 2048;

export type PageAlign = "left" | "right" | undefined;

/**
 * WebGL draws a page image straight over the sheet, so an image whose aspect
 * ratio differs from the book's page cell would stretch. Draw it into a canvas
 * with the cell's ratio instead, contained: the margins stay transparent and
 * the shader fills them with paper. Nothing is cropped, and a page whose shape
 * differs simply shows paper margins. In a spread, `align` pushes those
 * margins to the outer edge so the spine seam stays flush.
 */
export function fitPageTexture(
  image: HTMLImageElement,
  pageRatio: number,
  align?: PageAlign,
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
  const scale = Math.min(canvasWidth / width, canvasHeight / height);
  const drawWidth = width * scale;
  const drawHeight = height * scale;
  const x =
    align === "left"
      ? 0
      : align === "right"
        ? canvasWidth - drawWidth
        : (canvasWidth - drawWidth) / 2;
  ctx.drawImage(
    image,
    x,
    (canvasHeight - drawHeight) / 2,
    drawWidth,
    drawHeight,
  );
  return canvas;
}
