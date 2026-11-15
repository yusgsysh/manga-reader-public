/** Largest texture edge kept when letterboxing a page for the GPU. */
const MAX_TEXTURE = 2048;

/**
 * An image within this share of the page cell's aspect ratio is scaled to fill
 * the cell (`cover`); the small crop removes the paper margin that would
 * otherwise show as a white rim while a page peeks or turns. A larger
 * difference (a double-page spread) is contained so no content is cut off.
 */
const COVER_TOLERANCE = 0.05;

export type PageFit = "contain" | "cover";

export function pageFit(imageRatio: number, pageRatio: number): PageFit {
  if (
    !Number.isFinite(imageRatio) ||
    imageRatio <= 0 ||
    !Number.isFinite(pageRatio) ||
    pageRatio <= 0
  ) {
    return "contain";
  }
  return Math.abs(imageRatio - pageRatio) / pageRatio <= COVER_TOLERANCE
    ? "cover"
    : "contain";
}

/**
 * WebGL draws a page image straight over the sheet, so an image whose aspect
 * ratio differs from the book's page cell would stretch. Draw it into a canvas
 * with the cell's ratio first: near-matching images cover it (no white rim),
 * very different ones are contained with transparent margins that the shader
 * fills with paper.
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
  const imageRatio = width / height;
  // Already the page's shape: the GPU can sample it directly.
  if (Math.abs(imageRatio / pageRatio - 1) < 0.0005) return image;

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
  const fit = pageFit(imageRatio, pageRatio);
  const scale =
    fit === "cover"
      ? Math.max(canvasWidth / width, canvasHeight / height)
      : Math.min(canvasWidth / width, canvasHeight / height);
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
