// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createPasskey,
  decodeBase64Url,
  encodeBase64Url,
  getPasskey,
} from "./webauthn";

function bytes(...values: number[]): ArrayBuffer {
  return new Uint8Array(values).buffer;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("base64url", () => {
  it("round-trips arbitrary bytes without padding", () => {
    const value = new Uint8Array([0, 1, 2, 250, 251, 252, 253, 254, 255]);
    const encoded = encodeBase64Url(value);
    expect(encoded).not.toContain("=");
    expect(encoded).not.toContain("+");
    expect(encoded).not.toContain("/");
    expect(Array.from(decodeBase64Url(encoded))).toEqual(Array.from(value));
  });
});

describe("createPasskey", () => {
  it("decodes the options and encodes the attestation response", async () => {
    const create = vi.fn(async () => ({
      id: "credential-id",
      rawId: bytes(9, 8, 7),
      type: "public-key",
      authenticatorAttachment: "platform",
      response: {
        clientDataJSON: bytes(1, 2, 3),
        attestationObject: bytes(4, 5, 6),
        getTransports: () => ["internal", "hybrid"],
      },
      getClientExtensionResults: () => ({ credProps: { rk: true } }),
    }));
    Object.defineProperty(navigator, "credentials", {
      configurable: true,
      value: { create, get: vi.fn() },
    });

    const result = (await createPasskey({
      publicKey: {
        challenge: encodeBase64Url(bytes(10, 11, 12)),
        rp: { id: "example.com", name: "Manga Reader" },
        user: { id: encodeBase64Url(bytes(1)), name: "admin", displayName: "admin" },
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        excludeCredentials: [{ id: encodeBase64Url(bytes(2)), type: "public-key" }],
      },
    })) as {
      id: string;
      rawId: string;
      response: { clientDataJSON: string; attestationObject: string; transports: string[] };
    };

    const options = (create.mock.calls[0] as unknown[])[0] as {
      publicKey: { challenge: Uint8Array; user: { id: Uint8Array }; excludeCredentials: { id: Uint8Array }[] };
    };
    expect(Array.from(options.publicKey.challenge)).toEqual([10, 11, 12]);
    expect(Array.from(options.publicKey.user.id)).toEqual([1]);
    expect(Array.from(options.publicKey.excludeCredentials[0].id)).toEqual([2]);

    expect(result.id).toBe("credential-id");
    expect(result.rawId).toBe(encodeBase64Url(bytes(9, 8, 7)));
    expect(result.response.clientDataJSON).toBe(encodeBase64Url(bytes(1, 2, 3)));
    expect(result.response.attestationObject).toBe(encodeBase64Url(bytes(4, 5, 6)));
    expect(result.response.transports).toEqual(["internal", "hybrid"]);
  });
});

describe("getPasskey", () => {
  it("decodes allowCredentials and encodes the assertion response", async () => {
    const get = vi.fn(async () => ({
      id: "credential-id",
      rawId: bytes(3, 2, 1),
      type: "public-key",
      authenticatorAttachment: "platform",
      response: {
        clientDataJSON: bytes(1),
        authenticatorData: bytes(2),
        signature: bytes(3),
        userHandle: bytes(4),
      },
      getClientExtensionResults: () => ({}),
    }));
    Object.defineProperty(navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(), get },
    });

    const result = (await getPasskey({
      publicKey: {
        challenge: encodeBase64Url(bytes(7)),
        allowCredentials: [{ id: encodeBase64Url(bytes(5)), type: "public-key" }],
      },
    })) as {
      response: { userHandle: string; signature: string };
    };

    const options = (get.mock.calls[0] as unknown[])[0] as {
      publicKey: { challenge: Uint8Array; allowCredentials: { id: Uint8Array }[] };
    };
    expect(Array.from(options.publicKey.challenge)).toEqual([7]);
    expect(Array.from(options.publicKey.allowCredentials[0].id)).toEqual([5]);
    expect(result.response.userHandle).toBe(encodeBase64Url(bytes(4)));
    expect(result.response.signature).toBe(encodeBase64Url(bytes(3)));
  });
});
