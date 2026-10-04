import { apiGet, apiPut } from "./client";

/**
 * Sentinel the backend returns instead of a stored secret. Echoing it back in a
 * save means "keep what you already have", so a form that round-trips the
 * masked value can never wipe a credential.
 */
export const SETTINGS_MASK = "********";

export interface CookieSettings {
  memberId: string;
  passHash: string;
  igneous: string;
  sk: string;
  /** True once both ipb_member_id and ipb_pass_hash are set. */
  configured: boolean;
}

export interface S3Settings {
  endpoint: string;
  region: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  useSsl: boolean;
  pathStyle: string;
}

export interface StorageSettings {
  /** Configured driver: auto, local or s3. */
  driver: string;
  /** What actually runs right now: local or s3. */
  resolvedDriver: string;
  dir: string;
  s3: S3Settings;
}

/** The effective configuration the settings page edits. Secrets are masked. */
export interface SettingsSnapshot {
  /** True once anything has been overridden through this API. */
  persisted: boolean;
  cookie: CookieSettings;
  storage: StorageSettings;
  logLevel: string;
  devTools: boolean;
}

/**
 * PATCH-style payload: an absent key leaves that setting unchanged. Secret
 * fields may carry SETTINGS_MASK to mean "unchanged" as well.
 */
export interface SettingsUpdate {
  cookie?: {
    memberId?: string;
    passHash?: string;
    igneous?: string;
    sk?: string;
  };
  storage?: {
    driver?: string;
    dir?: string;
    s3?: {
      endpoint?: string;
      region?: string;
      bucket?: string;
      accessKey?: string;
      secretKey?: string;
      useSsl?: boolean;
      pathStyle?: string;
    };
  };
  logLevel?: string;
  devTools?: boolean;
}

export function fetchSettings(): Promise<SettingsSnapshot> {
  return apiGet<SettingsSnapshot>("/api/settings");
}

export function saveSettings(
  update: SettingsUpdate,
): Promise<SettingsSnapshot> {
  return apiPut<SettingsSnapshot>("/api/settings", update);
}
