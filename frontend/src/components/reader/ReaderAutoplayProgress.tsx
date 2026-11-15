/**
 * The gray progress line comimi draws along the bottom edge while auto page
 * turn is running. comimi renders its own for slide mode; book mode mounts this
 * copy, styled and timed the same way, so both viewers show one bar while the
 * reading chrome is hidden.
 */
export interface ReaderAutoplayProgressProps {
  enabled: boolean;
  /** Whether the reader chrome is up; the bar only shows when it is hidden. */
  overlayVisible: boolean;
  intervalMs: number;
}

export function ReaderAutoplayProgress({
  enabled,
  overlayVisible,
  intervalMs,
}: ReaderAutoplayProgressProps) {
  if (!enabled) return null;

  return (
    <div
      className="reader-autoplay-progress"
      data-visible={!overlayVisible}
      aria-hidden="true"
    >
      <span
        className="reader-autoplay-progress-bar"
        style={{ animationDuration: `${Math.max(1000, intervalMs)}ms` }}
      />
    </div>
  );
}
