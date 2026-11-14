import { Loader } from "@cloudflare/kumo";

interface LoadingScreenProps {
  /** Optional message shown below the spinner. */
  message?: string;
}

// Shared by every page-level spinner so the circle keeps the same size and
// lands on the same pixel no matter which loading state is on screen.
const CENTERED_SPINNER =
  "absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2";

function Spinner({ label }: { label: string }) {
  return <Loader size={32} aria-label={label} className={CENTERED_SPINNER} />;
}

/**
 * Full-viewport loading state shared by every page-level spinner.
 *
 * The spinner is always centered in the viewport, while the message is
 * absolutely positioned out of flow. That way the presence, absence, or
 * length of the message can never shift the spinner — switching between
 * loading screens (route fallback, auth check, page data) keeps the circle in
 * exactly the same spot instead of making it jump.
 */
export function LoadingScreen({ message }: LoadingScreenProps) {
  return (
    <div className="relative grid h-dvh place-items-center bg-kumo-base">
      <Spinner label={message ?? "Loading"} />
      {message ? (
        <p className="absolute top-1/2 left-1/2 -translate-x-1/2 translate-y-6 text-sm whitespace-nowrap text-kumo-subtle">
          {message}
        </p>
      ) : null}
    </div>
  );
}

/**
 * Loading state for a lazily loaded page inside the app shell.
 *
 * It uses the same centered spinner as {@link LoadingScreen} and is fixed to
 * the viewport, so the header and main padding cannot move it. That keeps the
 * circle still through the whole boot sequence: auth check -> shell mounts ->
 * route chunk loads. Keep it a sibling of (not a child of) PageTransition: the
 * enter animation's transform would otherwise make this fixed element position
 * itself against the page box instead of the viewport, which is what made the
 * spinner jump to the top half.
 */
export function PageLoading() {
  return (
    <div className="pointer-events-none fixed inset-0">
      <Spinner label="Loading" />
    </div>
  );
}
