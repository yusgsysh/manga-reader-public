import { Loader } from "@cloudflare/kumo";

interface LoadingScreenProps {
  /** Optional message shown below the spinner. */
  message?: string;
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
      <Loader
        size={32}
        aria-label={message ?? "Loading"}
        className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2"
      />
      {message ? (
        <p className="absolute top-1/2 left-1/2 -translate-x-1/2 translate-y-6 text-sm whitespace-nowrap text-kumo-subtle">
          {message}
        </p>
      ) : null}
    </div>
  );
}
