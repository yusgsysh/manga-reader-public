import {
  apiDelete,
  apiGet,
  apiPatchJson,
  apiPostJson,
} from "./client";
import { createPasskey, getPasskey } from "../lib/webauthn";

/** Public shape returned by the passkey management endpoints. */
export interface PasskeySummary {
  id: string;
  name: string;
  created_at: string;
  last_used_at: string | null;
  transports: string[];
  backup_eligible: boolean;
  backup_state: boolean;
  user_verified: boolean;
  clone_warning: boolean;
}

interface PasskeyListResponse {
  passkeys: PasskeySummary[];
}

interface PasskeyResponse {
  passkey: PasskeySummary;
}

/** Same token/identity payload as password login. */
export interface PasskeyLoginResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  user: {
    id: string;
    user_id: string;
    username: string;
    role: string;
  };
}

interface CreationOptions {
  publicKey: {
    challenge: string;
    user: { id: string; [key: string]: unknown };
    excludeCredentials?: { id: string; [key: string]: unknown }[];
    [key: string]: unknown;
  };
  mediation?: CredentialMediationRequirement;
}

interface RequestOptions {
  publicKey: {
    challenge: string;
    allowCredentials?: { id: string; [key: string]: unknown }[];
    [key: string]: unknown;
  };
  mediation?: CredentialMediationRequirement;
}

export async function beginPasskeyLogin(): Promise<RequestOptions> {
  return apiPostJson<RequestOptions>("/api/auth/passkey/login/begin");
}

export async function finishPasskeyLogin(
  credential: unknown,
): Promise<PasskeyLoginResponse> {
  return apiPostJson<PasskeyLoginResponse>(
    "/api/auth/passkey/login/finish",
    credential,
  );
}

/** Runs the whole passwordless login ceremony. */
export async function loginWithPasskey(): Promise<PasskeyLoginResponse> {
  const options = await beginPasskeyLogin();
  const credential = await getPasskey(options);
  return finishPasskeyLogin(credential);
}

export function fetchPasskeys(): Promise<PasskeyListResponse> {
  return apiGet<PasskeyListResponse>("/api/auth/passkeys");
}

/**
 * Registers a new passkey for the signed-in account. The optional name is
 * passed as a query parameter because the finish body itself is the WebAuthn
 * attestation JSON.
 */
export async function registerPasskey(name?: string): Promise<PasskeyResponse> {
  const options = await apiPostJson<CreationOptions>(
    "/api/auth/passkeys/register/begin",
  );
  const credential = await createPasskey(options);
  const query = name?.trim()
    ? `?name=${encodeURIComponent(name.trim())}`
    : "";
  return apiPostJson<PasskeyResponse>(
    `/api/auth/passkeys/register/finish${query}`,
    credential,
  );
}

export function renamePasskey(
  id: string,
  name: string,
): Promise<PasskeyResponse> {
  return apiPatchJson<PasskeyResponse>(
    `/api/auth/passkeys/${encodeURIComponent(id)}`,
    { name },
  );
}

export function deletePasskey(id: string): Promise<{ success: boolean }> {
  return apiDelete<{ success: boolean }>(
    `/api/auth/passkeys/${encodeURIComponent(id)}`,
  );
}
