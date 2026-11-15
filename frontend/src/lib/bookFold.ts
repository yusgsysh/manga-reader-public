/**
 * Grabfold-style page-fold geometry and physics, adapted for comimi.
 *
 * Adapted from grabfold (https://github.com/javocsoft/grabfold, MIT License
 * © JavocSoft). The fold is modelled as the perpendicular bisector between the
 * grab point on the free edge and the current pointer, with the flat part of
 * the page clipped in place and the folded part reflected across the crease.
 *
 * These are pure functions re-implemented from grabfold's documented core
 * ideas (`foldGeometry`, `foldDepth`, half-plane clipping, reflection, flick
 * and peek physics). They describe where the crease falls, how the flat and
 * folded parts are clipped, where the folded face lands, and how fast a flick
 * sends the page over.
 */

export interface Point {
  x: number;
  y: number;
}

export interface FoldInput {
  /** The cell edge the sheet is bound on. */
  spine: "left" | "right";
  /** Page cell width, in pixels. */
  width: number;
  /** Page cell height, in pixels. */
  height: number;
  /** 0 lying flat on its own page, 1 folded flat onto the facing one. */
  progress: number;
  /** How far down the free edge the sheet was taken hold of, in pixels. */
  grabY: number;
  /** How far the pointer has since strayed from that height, in pixels. */
  lift: number;
}

export interface Strip {
  x: number;
  y: number;
  angle: number;
  span: number;
  length: number;
}

export interface Fold {
  /** The part still lying flat, in cell coordinates. */
  leaf: Point[];
  /** What the fold has uncovered, in cell coordinates. */
  uncovered: Point[];
  /** The part folded over, in the folded face's own coordinates. */
  flap: Point[];
  /** Puts the folded face where the fold leaves it. */
  place: { x: number; y: number; angle: number };
  /** The roll of the crease, in the folded face's own coordinates. */
  roll: Strip;
  /** What the standing fold throws on the page, in cell coordinates. */
  cast: Strip;
  /** The crease line, in cell coordinates. */
  crease: { through: Point; normal: Point };
}

const SHADOW_SPAN = 0.42;
const ROLL_SHARE = 0.75;
const LEAST_MOVE = 0.5;
const MAX_LEAN = 1.6;

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}

export function clipHalfPlane(
  polygon: readonly Point[],
  side: (point: Point) => number,
): Point[] {
  const kept: Point[] = [];

  for (let index = 0; index < polygon.length; index += 1) {
    const from = polygon[index];
    const to = polygon[(index + 1) % polygon.length];
    const here = side(from);
    const there = side(to);

    if (here >= 0) kept.push(from);
    if ((here >= 0) !== (there >= 0)) {
      const at = here / (here - there);
      kept.push({
        x: from.x + (to.x - from.x) * at,
        y: from.y + (to.y - from.y) * at,
      });
    }
  }

  return kept;
}

export function reflect(point: Point, through: Point, normal: Point): Point {
  const square = normal.x * normal.x + normal.y * normal.y;
  const over =
    ((point.x - through.x) * normal.x + (point.y - through.y) * normal.y) /
    square;
  return {
    x: point.x - 2 * over * normal.x,
    y: point.y - 2 * over * normal.y,
  };
}

function strip(through: Point, normal: Point, span: number, length: number): Strip {
  const size = Math.hypot(normal.x, normal.y);
  const acrossX = -normal.x / size;
  const acrossY = -normal.y / size;
  const alongX = -acrossY;
  const alongY = acrossX;

  return {
    x: through.x - alongX * (length / 2),
    y: through.y - alongY * (length / 2),
    angle: Math.atan2(acrossY, acrossX),
    span,
    length,
  };
}

export function foldGeometry(input: FoldInput): Fold {
  const { spine, width, height } = input;
  const progress = clamp(input.progress, 0, 1);

  const carried = Math.max(LEAST_MOVE, width * 2 * progress);
  const grab: Point = { x: spine === "left" ? width : 0, y: input.grabY };
  const reached = spine === "left" ? width - carried : carried;

  const spineX = spine === "left" ? 0 : width;
  const gap = reached - spineX;
  const roomAt = (cornerY: number) => {
    const bound = Math.hypot(width, input.grabY - cornerY);
    return Math.sqrt(Math.max(0, bound * bound - gap * gap));
  };
  const fromTop = roomAt(0);
  const fromBottom = roomAt(height);

  const lean = MAX_LEAN * carried;
  const point: Point = {
    x: reached,
    y: clamp(
      input.grabY + input.lift,
      Math.max(-fromTop, height - fromBottom, input.grabY - lean),
      Math.min(fromTop, height + fromBottom, input.grabY + lean),
    ),
  };

  const normal: Point = { x: point.x - grab.x, y: point.y - grab.y };
  const middle: Point = {
    x: (grab.x + point.x) / 2,
    y: (grab.y + point.y) / 2,
  };

  const page: Point[] = [
    { x: 0, y: 0 },
    { x: width, y: 0 },
    { x: width, y: height },
    { x: 0, y: height },
  ];

  const side = (at: Point) =>
    (at.x - middle.x) * normal.x + (at.y - middle.y) * normal.y;

  const leaf = clipHalfPlane(page, side);
  const uncovered = clipHalfPlane(page, (at) => -side(at));

  const mirror = (at: Point): Point => ({ x: width - at.x, y: at.y });
  const carry = (at: Point) => reflect(mirror(at), middle, normal);

  const origin = carry({ x: 0, y: 0 });
  const along = carry({ x: 1, y: 0 });

  const depth = Math.sin(Math.PI * progress);
  const span = SHADOW_SPAN * width * depth;
  const reach = 2 * Math.hypot(width, height);

  return {
    leaf,
    uncovered,
    flap: uncovered.map(mirror),
    place: {
      x: origin.x,
      y: origin.y,
      angle: Math.atan2(along.y - origin.y, along.x - origin.x),
    },
    roll: strip(
      mirror(middle),
      { x: -normal.x, y: normal.y },
      span * ROLL_SHARE,
      reach,
    ),
    cast: strip(middle, normal, span, reach),
    crease: {
      through: middle,
      normal: {
        x: normal.x / Math.hypot(normal.x, normal.y),
        y: normal.y / Math.hypot(normal.x, normal.y),
      },
    },
  };
}

export function foldDepth(progress: number): number {
  return Math.sin(Math.PI * clamp(progress, 0, 1));
}

export interface Sample {
  time: number;
  progress: number;
}

export const FLICK_WINDOW = 90;
export const FLICK_SPEED = 0.0004;
export const FLICK_MIN_PROGRESS = 0.02;

export function releaseSpeed(
  samples: readonly Sample[],
  window = FLICK_WINDOW,
): number {
  if (samples.length < 2) return 0;
  const last = samples[samples.length - 1];
  let first = last;
  for (let index = samples.length - 2; index >= 0; index -= 1) {
    const sample = samples[index];
    if (last.time - sample.time > window) break;
    first = sample;
  }
  const elapsed = last.time - first.time;
  return elapsed > 0 ? (last.progress - first.progress) / elapsed : 0;
}

export function releaseTarget(
  progress: number,
  speed: number,
  commitAt: number,
): 0 | 1 {
  if (Math.abs(speed) >= FLICK_SPEED) {
    if (speed > 0 && progress >= FLICK_MIN_PROGRESS) return 1;
    if (speed < 0) return 0;
  }
  return progress > commitAt ? 1 : 0;
}

export function settleDuration(
  remaining: number,
  fullTurn: number,
  speed = 0,
  floor = 120,
): number {
  const stock = fullTurn * Math.abs(remaining);
  if (Math.abs(speed) < FLICK_SPEED || remaining === 0) return stock;
  const matched = (3 * Math.abs(remaining)) / Math.abs(speed);
  return Math.max(Math.min(floor, stock), Math.min(stock, matched));
}

export const PEEK_MAX = 0.06;

export function peekProgress(
  distance: number,
  zone: number,
  max = PEEK_MAX,
): number {
  if (zone <= 0 || distance < 0 || distance > zone) return 0;
  const closeness = 1 - distance / zone;
  return max * closeness * closeness * (3 - 2 * closeness);
}

/** Builds a CSS `polygon(...)` string from pixel points. */
export function polygon(points: readonly Point[]): string {
  if (points.length < 3) return "polygon(0 0, 0 0, 0 0)";
  return `polygon(${points
    .map((point) => `${point.x.toFixed(2)}px ${point.y.toFixed(2)}px`)
    .join(", ")})`;
}
