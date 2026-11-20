import { apiGet, apiPostJson, setAccessToken } from "./client";
import { loginWithPasskey as passkeyLoginRequest } from "./passkey";

export interface AuthUser {
  id: string;
  user_id: string;
  username: string;
  role: string;
}

interface AuthIdentity {
  user: AuthUser;
}

interface AuthStatus {
  initialized: boolean;
  /** Present when the probe carried a valid session; null when signed out. */
  user: UserPayload | null;
}

export interface AuthSession extends Omit<AuthStatus, "user"> {
  user: AuthUser | null;
}

interface LoginSuccessResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  user: UserPayload;
}

interface MFAChallengeResponse {
  mfa_required: true;
  mfa_token: string;
  expires_in: number;
  method: "totp";
}

export type LoginResult =
  | { mfaRequired: false; user: AuthUser }
  | { mfaRequired: true; mfaToken: string };

export interface TotpStatus {
  enabled: boolean;
  confirmed_at: string | null;
  recovery_codes_remaining: number;
}

export interface TotpSetup {
  secret: string;
  otpauth_url: string;
  qr_data_url: string;
  issuer: string;
  account_name: string;
}

export interface TotpEnableResult {
  enabled: boolean;
  confirmed_at: string;
  recovery_codes: string[];
}

/** Identity shape returned by /api/auth/status, login and register. */
interface UserPayload {
  id: string;
  user_id: string;
  username: string;
  role: string;
}

function fetchAuthStatus(): Promise<AuthStatus> {
  return apiGet<AuthStatus>("/api/auth/status");
}

function toAuthUser(response: UserPayload): AuthUser {
  return {
    id: response.id,
    user_id: response.user_id ?? response.id,
    username: response.username,
    role: response.role,
  };
}

/**
 * Resolves the complete auth state used by the SPA.
 *
 * The JWT is carried by an HttpOnly cookie after login, so a page refresh only
 * needs one probe: /api/auth/status reports both the first-run state and, when
 * the cookie is valid, the signed-in identity. Doing this in one request
 * shortens the boot spinner by a full round-trip; `user: null` simply means
 * "no session yet".
 */
export async function fetchAuthSession(): Promise<AuthSession> {
  const status = await fetchAuthStatus();
  return {
    initialized: status.initialized,
    user: status.user ? toAuthUser(status.user) : null,
  };
}

export async function login(
  username: string,
  password: string,
): Promise<LoginResult> {
  const result = await apiPostJson<LoginSuccessResponse | MFAChallengeResponse>(
    "/api/auth/login",
    { username, password },
  );
  if ("mfa_required" in result) {
    return { mfaRequired: true, mfaToken: result.mfa_token };
  }
  // Also keep the token in memory so same-tab API calls can send an
  // Authorization header. Persistence is the HttpOnly cookie's job.
  setAccessToken(result.access_token);
  // The identity rides along with the token, so there is no follow-up /me
  // request that could fail and leave a half-applied login.
  return { mfaRequired: false, user: toAuthUser(result.user) };
}

/**
 * Completes a password login that returned an MFA challenge. The step-up token
 * is short-lived and only accepted by this endpoint; recovery codes are sent in
 * the same field as a six-digit TOTP code.
 */
export async function verifyTOTP(
  mfaToken: string,
  code: string,
): Promise<AuthIdentity> {
  const result = await apiPostJson<LoginSuccessResponse>(
    "/api/auth/totp/verify",
    { mfa_token: mfaToken, code },
  );
  setAccessToken(result.access_token);
  return { user: toAuthUser(result.user) };
}

/**
 * Passwordless login: discoverable WebAuthn credential -> session.
 *
 * Unlike `login`, this never sends a username: the authenticator selects the
 * credential and the server resolves its owner from the credential id.
 */
export async function loginWithPasskey(): Promise<AuthIdentity> {
  const result = await passkeyLoginRequest();
  setAccessToken(result.access_token);
  return { user: toAuthUser(result.user) };
}

/**
 * Creates the first administrator while the instance is still uninitialized.
 * On success the backend also issues a session, so the caller is logged in.
 */
export async function register(
  username: string,
  password: string,
): Promise<AuthIdentity> {
  const result = await apiPostJson<LoginSuccessResponse>("/api/auth/register", {
    username,
    password,
  });
  setAccessToken(result.access_token);
  return { user: toAuthUser(result.user) };
}

export async function logout(): Promise<void> {
  try {
    await apiPostJson("/api/auth/logout");
  } finally {
    setAccessToken(null);
  }
}
