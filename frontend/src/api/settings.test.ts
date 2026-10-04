import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchSettings, saveSettings, SETTINGS_MASK } from "./settings";
import { ApiRequestError } from "./client";

const BASE = "http://localhost:8080";

const fetchMock = vi.fn();
const originalFetch = globalThis.fetch;

beforeEach(() => {
  fetchMock.mockReset();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const snapshot = {
  persisted: false,
  cookie: {
    memberId: "42",
    passHash: SETTINGS_MASK,
    igneous: "",
    sk: "",
    configured: true,
  },
  storage: {
    driver: "auto",
    resolvedDriver: "local",
    dir: "data/cache",
    s3: {
      endpoint: "",
      region: "",
      bucket: "",
      accessKey: "",
      secretKey: "",
      useSsl: false,
      pathStyle: "auto",
    },
  },
  logLevel: "warn",
  devTools: false,
};

describe("fetchSettings", () => {
  it("reads the settings snapshot", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(snapshot));

    const got = await fetchSettings();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`${BASE}/api/settings`);
    expect(init?.method ?? "GET").toBe("GET");
    expect(got.cookie.passHash).toBe(SETTINGS_MASK);
    expect(got.logLevel).toBe("warn");
  });
});

describe("saveSettings", () => {
  it("sends only the supplied keys as a JSON PUT", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ ...snapshot, logLevel: "debug", persisted: true }),
    );

    const saved = await saveSettings({ logLevel: "debug" });

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`${BASE}/api/settings`);
    expect(init?.method).toBe("PUT");
    expect(init?.headers).toEqual({ "Content-Type": "application/json" });
    expect(init?.body).toBe(JSON.stringify({ logLevel: "debug" }));
    expect(saved.persisted).toBe(true);
  });

  it("round-trips masked secrets untouched so they are not overwritten", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(snapshot));

    await saveSettings({
      cookie: { memberId: "42", passHash: SETTINGS_MASK },
    });

    const [, init] = fetchMock.mock.calls[0]!;
    expect(JSON.parse(init!.body as string)).toEqual({
      cookie: { memberId: "42", passHash: SETTINGS_MASK },
    });
  });

  it("surfaces a rejected save as a 400 ApiRequestError", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        { error: "ExHentai cookie is incomplete" },
        400,
      ),
    );

    const err = await saveSettings({ cookie: { passHash: "" } }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiRequestError);
    expect(err).toMatchObject({
      status: 400,
      message: "ExHentai cookie is incomplete",
    });
  });
});
