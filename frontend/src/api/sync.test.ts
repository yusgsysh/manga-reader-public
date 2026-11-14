import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SYNC_KEY_MASK,
  createUserSyncKey,
  fetchSyncConfig,
  fetchSyncStatus,
  fetchUserSyncKeys,
  revokeUserSyncKey,
  saveSyncConfig,
  startFullSync,
} from "./sync";
import type { CreatedUserSyncKey, SyncConfig, SyncStatus, UserSyncKey } from "./sync";

const BASE = "http://localhost:8080";

const sampleConfig: SyncConfig = {
  enabled: true,
  server_url: "https://server.example.com",
  token: SYNC_KEY_MASK,
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
  host_enabled: false,
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

  it("startFullSync POSTs /api/sync/resync", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await expect(startFullSync()).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledWith(`${BASE}/api/sync/resync`, {
      method: "POST",
    });
  });

  it("user sync key helpers use /api/sync/keys", async () => {
    const key: UserSyncKey = {
      id: "key-1",
      key_prefix: "mrk_abcdefgh",
      status: "active",
      expires_at: null,
      last_used_at: null,
      created_at: "2026-01-01T00:00:00Z",
      revoked_at: null,
    };
    const created: CreatedUserSyncKey = { key: "mrk_secret", api_key: key };

    fetchMock.mockResolvedValueOnce(jsonResponse({ keys: [key] }));
    await expect(fetchUserSyncKeys()).resolves.toEqual({ keys: [key] });
    expect(fetchMock).toHaveBeenLastCalledWith(`${BASE}/api/sync/keys`, undefined);

    fetchMock.mockResolvedValueOnce(jsonResponse(created, 201));
    await expect(createUserSyncKey()).resolves.toEqual(created);
    expect(fetchMock).toHaveBeenLastCalledWith(`${BASE}/api/sync/keys`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: undefined,
    });

    fetchMock.mockResolvedValueOnce(jsonResponse({ ...key, status: "revoked" }));
    await expect(revokeUserSyncKey("key-1")).resolves.toEqual({
      ...key,
      status: "revoked",
    });
    expect(fetchMock).toHaveBeenLastCalledWith(
      `${BASE}/api/sync/keys/key-1`,
      { method: "DELETE" },
    );
  });
});
