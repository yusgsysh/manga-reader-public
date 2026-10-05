import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SYNC_TOKEN_MASK,
  fetchSyncConfig,
  fetchSyncStatus,
  saveSyncConfig,
} from "./sync";
import type { SyncConfig, SyncStatus } from "./sync";

const BASE = "http://localhost:8080";

const sampleConfig: SyncConfig = {
  enabled: true,
  server_url: "https://server.example.com",
  token: SYNC_TOKEN_MASK,
};

const sampleStatus: SyncStatus = {
  enabled: true,
  configured: true,
  server_url: "https://server.example.com",
  sse_connected: true,
  last_sync_at: "2026-01-01T00:00:00Z",
  cursor: 12,
  last_pushed_id: 4,
  pending: 0,
};

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

describe("sync api", () => {
  it("fetchSyncConfig GETs /api/sync/config", async () => {
    fetchMock.mockResolvedValue(jsonResponse(sampleConfig));
    await expect(fetchSyncConfig()).resolves.toEqual(sampleConfig);
    expect(fetchMock).toHaveBeenCalledWith(`${BASE}/api/sync/config`, undefined);
  });

  it("saveSyncConfig PUTs the JSON body", async () => {
    fetchMock.mockResolvedValue(jsonResponse(sampleConfig));
    const update = { enabled: true, server_url: "https://server.example.com" };
    await expect(saveSyncConfig(update)).resolves.toEqual(sampleConfig);
    expect(fetchMock).toHaveBeenCalledWith(`${BASE}/api/sync/config`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(update),
    });
  });

  it("fetchSyncStatus GETs /api/sync/status", async () => {
    fetchMock.mockResolvedValue(jsonResponse(sampleStatus));
    await expect(fetchSyncStatus()).resolves.toEqual(sampleStatus);
    expect(fetchMock).toHaveBeenCalledWith(`${BASE}/api/sync/status`, undefined);
  });
});
