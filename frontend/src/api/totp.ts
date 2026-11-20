import { apiGet, apiPostJson } from "./client";
import type { TotpEnableResult, TotpSetup, TotpStatus } from "./auth";

export function fetchTotpStatus(): Promise<TotpStatus> {
  return apiGet<TotpStatus>("/api/auth/totp");
}

export function beginTotpSetup(): Promise<TotpSetup> {
  return apiPostJson<TotpSetup>("/api/auth/totp/setup");
}

export function enableTotp(code: string): Promise<TotpEnableResult> {
  return apiPostJson<TotpEnableResult>("/api/auth/totp/enable", { code });
}

export function disableTotp(
  password: string,
  code: string,
): Promise<{ enabled: false }> {
  return apiPostJson<{ enabled: false }>("/api/auth/totp/disable", {
    password,
    code,
  });
}

export function regenerateRecoveryCodes(
  password: string,
  code: string,
): Promise<{ recovery_codes: string[] }> {
  return apiPostJson<{ recovery_codes: string[] }>(
    "/api/auth/totp/recovery-codes",
    { password, code },
  );
}
