import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cancelPrefillJob,
  cleanupPrefillJobs,
  deletePrefillJob,
  fetchPrefillJobs,
  prefillZipUrl,
  startPrefillJob,
} from "./prefill";
import type { PrefillJob } from "../types/prefill";

const BASE = "http://localhost:8080";

const sampleJob: PrefillJob = {
  id: "11111111-2222-3333-4444-555555555555",
  gallery_id: 123,
  token: "tok",
  title: "title",
  status: "queued",
  total: 3,
  progress: null,
  failed_count: 0,
  errors: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  finished_at: null,
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

describe("prefill api", () => {
  it("startPrefillJob POSTs the JSON body to /api/prefill", async () => {
    fetchMock.mockResolvedValue(jsonResponse(sampleJob, 202));

    const req = {
      gallery_id: 123,
      token: "tok",
      title: "title",
      urls: ["https://exhentai.org/s/a/1-1"],
    };
    const job = await startPrefillJob(req);

    expect(job).toEqual(sampleJob);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`${BASE}/api/prefill`);
    expect(init.method).toBe("POST");
    expect(init.headers).toEqual({ "Content-Type": "application/json" });
    expect(JSON.parse(String(init.body))).toEqual(req);
  });

  it("fetchPrefillJobs GETs the job list", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ jobs: [sampleJob] }));
    const resp = await fetchPrefillJobs();
    expect(resp.jobs).toHaveLength(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit?];
    expect(url).toBe(`${BASE}/api/prefill`);
    expect(init?.method).toBeUndefined();
  });

  it("cancelPrefillJob and deletePrefillJob target the job routes", async () => {
    fetchMock.mockResolvedValue(jsonResponse(sampleJob));
    await cancelPrefillJob("11111111-2222-3333-4444-555555555555");
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[0]).toBe(
      `${BASE}/api/prefill/11111111-2222-3333-4444-555555555555/cancel`,
    );
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[1].method).toBe(
      "POST",
    );

    fetchMock.mockResolvedValue(jsonResponse({ deleted: 1 }));
    await deletePrefillJob("11111111-2222-3333-4444-555555555555");
    expect((fetchMock.mock.calls[1] as [string, RequestInit])[1].method).toBe(
      "DELETE",
    );
  });

  it("cleanupPrefillJobs sends days as a query parameter, including 0", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ days: 0, deleted: 4 }));
    const resp = await cleanupPrefillJobs(0);
    expect(resp.deleted).toBe(4);
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[0]).toBe(
      `${BASE}/api/prefill/cleanup?days=0`,
    );
  });

  it("prefillZipUrl builds the streaming zip URL", () => {
    expect(prefillZipUrl("11111111-2222-3333-4444-555555555555")).toBe(
      `${BASE}/api/prefill/11111111-2222-3333-4444-555555555555/zip`,
    );
  });

  it("surfaces the backend error envelope", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ error: "job already finished" }, 409),
    );
    await expect(cancelPrefillJob("7")).rejects.toThrow("job already finished");
  });
});
