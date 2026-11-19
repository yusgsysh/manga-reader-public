// Kinde configuration is public by design: domain, client id, audience and
// callback URLs are all visible to the browser. Client secrets must never be
// placed in VITE_* variables.

export interface KindeConfig {
  domain: string;
  clientId: string;
  redirectUri: string;
  logoutUri: string;
  audience: string;
}

function defaultOrigin(path: string): string {
  if (typeof window === "undefined") return path;
  return `${window.location.origin}${path}`;
}

// Kinde's authorize URL strips a trailing slash while the token exchange uses
// the value verbatim. Keeping the root callback slash-free avoids a mismatch.
function rootCallback(): string {
  if (typeof window === "undefined") return "/";
  return window.location.origin;
}

/** Returns null when the deployment forgot to configure Kinde. */
export function readKindeConfig(): KindeConfig | null {
  const domain = import.meta.env.VITE_KINDE_DOMAIN?.trim();
  const clientId = import.meta.env.VITE_KINDE_CLIENT_ID?.trim();
  const audience = import.meta.env.VITE_KINDE_AUDIENCE?.trim();
  if (!domain || !clientId || !audience) return null;
  return {
    domain,
    clientId,
    audience,
    redirectUri:
      import.meta.env.VITE_KINDE_REDIRECT_URI?.trim() || rootCallback(),
    logoutUri:
      import.meta.env.VITE_KINDE_LOGOUT_URI?.trim() || defaultOrigin("/login"),
  };
}

/** Test/e2e builds bypass Kinde and use a fixed local identity. */
export function isE2EAuth(): boolean {
  return import.meta.env.VITE_E2E_AUTH === "true";
}
