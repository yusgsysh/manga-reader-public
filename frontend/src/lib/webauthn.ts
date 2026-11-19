/**
 * Minimal WebAuthn JSON <-> browser API bridge.
 *
 * The backend returns go-webauthn's standard JSON form (base64url-encoded
 * challenge/ids). The browser expects BufferSource values, so these helpers
 * decode the options on the way in and encode the credential on the way out.
 * Keeping this local avoids a runtime dependency on @simplewebauthn/browser
 * while using only the platform API.
 */

interface CredentialDescriptorJSON {
  id: string;
  type?: string;
  transports?: string[];
}

interface PublicKeyCreationJSON {
  publicKey: {
    challenge: string;
    rp?: { id?: string; name?: string };
    user: { id: string; name?: string; displayName?: string };
    excludeCredentials?: CredentialDescriptorJSON[];
    [key: string]: unknown;
  };
  mediation?: CredentialMediationRequirement;
}

interface PublicKeyRequestJSON {
  publicKey: {
    challenge: string;
    allowCredentials?: CredentialDescriptorJSON[];
    [key: string]: unknown;
  };
  mediation?: CredentialMediationRequirement;
}

/** True when the browser exposes the WebAuthn API (requires a secure context). */
export function isPasskeySupported(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext === true &&
    typeof window.PublicKeyCredential !== "undefined" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials !== "undefined"
  );
}

export function decodeBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized.padEnd(
    normalized.length + ((4 - (normalized.length % 4)) % 4),
    "=",
  );
  const binary = atob(padded);
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export function encodeBase64Url(value: ArrayBuffer | Uint8Array): string {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  let binary = "";
  for (let i = 0; i < bytes.length; i += 1) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function toDescriptor(
  descriptor: CredentialDescriptorJSON,
): PublicKeyCredentialDescriptor {
  return {
    id: decodeBase64Url(descriptor.id),
    type: "public-key",
    transports: descriptor.transports as AuthenticatorTransport[] | undefined,
  };
}

/** Runs navigator.credentials.create() and returns the JSON the server parses. */
export async function createPasskey(
  options: PublicKeyCreationJSON,
): Promise<unknown> {
  const { publicKey } = options;
  const browserOptions = {
    ...publicKey,
    challenge: decodeBase64Url(publicKey.challenge),
    user: { ...publicKey.user, id: decodeBase64Url(publicKey.user.id) },
    excludeCredentials: publicKey.excludeCredentials?.map(toDescriptor),
  } as unknown as PublicKeyCredentialCreationOptions;

  const credential = (await navigator.credentials.create({
    publicKey: browserOptions,
  })) as PublicKeyCredential | null;
  if (!credential) {
    throw new Error("浏览器未返回通行密钥");
  }
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: encodeBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: encodeBase64Url(response.clientDataJSON),
      attestationObject: encodeBase64Url(response.attestationObject),
      transports: response.getTransports?.() ?? [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

/** Runs navigator.credentials.get() and returns the JSON the server parses. */
export async function getPasskey(
  options: PublicKeyRequestJSON,
): Promise<unknown> {
  const { publicKey } = options;
  const browserOptions = {
    ...publicKey,
    challenge: decodeBase64Url(publicKey.challenge),
    allowCredentials: publicKey.allowCredentials?.map(toDescriptor),
  } as unknown as PublicKeyCredentialRequestOptions;

  const credential = (await navigator.credentials.get({
    publicKey: browserOptions,
    mediation: options.mediation,
  })) as PublicKeyCredential | null;
  if (!credential) {
    throw new Error("未选择通行密钥");
  }
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: encodeBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: encodeBase64Url(response.clientDataJSON),
      authenticatorData: encodeBase64Url(response.authenticatorData),
      signature: encodeBase64Url(response.signature),
      userHandle: response.userHandle
        ? encodeBase64Url(response.userHandle)
        : null,
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}
