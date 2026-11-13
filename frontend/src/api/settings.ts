import { apiDelete, apiGet, apiPostJson, apiPut } from "./client";

export type CookieStatus = "unset" | "unverified" | "valid" | "invalid";
export type ExhentaiSite = "exhentai" | "e-hentai";

export interface CookieSummary {
  configured: boolean;
  status: CookieStatus;
  site: ExhentaiSite;
  verified_at?: string;
  updated_at?: string;
  has_igneous: boolean;
  has_sk: boolean;
}

export interface PersonalSettings {
  preferences: Record<string, unknown>;
  site: ExhentaiSite;
  cookie: CookieSummary
}

export interface CookieUpdate {
  cookie?: string;
  ipb_member_id?: string;
  ipb_pass_hash?: string;
  igneous?: string;
  sk?: string;
  site?: ExhentaiSite;
}

export function fetchSettings(): Promise<PersonalSettings> {
  return apiGet<PersonalSettings>("/api/settings");
}

export function saveSettings(input: {
  preferences: Record<string, unknown>;
  site?: ExhentaiSite;
}): Promise<PersonalSettings> {
  return apiPut<PersonalSettings>("/api/settings", input);
}

export function fetchCookie(): Promise<CookieSummary> {
  return apiGet<CookieSummary>("/api/settings/cookie");
}

export function saveCookie(input: CookieUpdate): Promise<CookieSummary> {
  return apiPut<CookieSummary>("/api/settings/cookie", input);
}

export function verifyCookie(): Promise<CookieSummary> {
  return apiPostJson<CookieSummary>("/api/settings/cookie/verify");
}

export function clearCookie(): Promise<CookieSummary> {
  return apiDelete<CookieSummary>("/api/settings/cookie");
}
