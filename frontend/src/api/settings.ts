import { apiDelete, apiGet, apiPostJson, apiPut } from "./client";

type CookieStatus = "unset" | "unverified" | "valid" | "invalid";
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

interface CookieUpdate {
  cookie?: string;
  ipb_member_id?: string;
  ipb_pass_hash?: string;
  igneous?: string;
  sk?: string;
  site?: ExhentaiSite;
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
