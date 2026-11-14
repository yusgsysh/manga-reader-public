import { apiDelete, apiGet, apiPostJson, apiPut } from "./client";

type NodeStatus = "active" | "disabled";
type NodeKeyStatus = "active" | "revoked";
/** Sync scopes are checked by default; metrics access is a separate opt-in. */
export const NODE_SCOPES = ["sync:read", "sync:write", "sync:events"] as const;
export const METRICS_SCOPE = "metrics:read" as const;
export type NodeScope = (typeof NODE_SCOPES)[number] | typeof METRICS_SCOPE;

export interface SyncNode {
  id: string;
  name: string;
  status: NodeStatus;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface NodeAPIKey {
  id: string;
  node_id: string;
  key_prefix: string;
  scopes: NodeScope[];
  status: NodeKeyStatus;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
  revoked_at: string | null;
}

interface CreatedNodeKey {
  /** Full plaintext key. Returned only once by the server. */
  key: string;
  api_key: NodeAPIKey;
  revoked_old?: boolean;
}

export function fetchNodes(): Promise<{ nodes: SyncNode[] }> {
  return apiGet<{ nodes: SyncNode[] }>("/api/admin/nodes");
}

export function createNode(input: {
  name: string;
  status?: NodeStatus;
}): Promise<SyncNode> {
  return apiPostJson<SyncNode>("/api/admin/nodes", input);
}

export function updateNode(
  id: string,
  input: { name?: string; status?: NodeStatus },
): Promise<SyncNode> {
  return apiPut<SyncNode>(`/api/admin/nodes/${id}`, input);
}

export function deleteNode(id: string): Promise<{ deleted: boolean }> {
  return apiDelete<{ deleted: boolean }>(`/api/admin/nodes/${id}`);
}

export function fetchNodeKeys(
  id: string,
): Promise<{ keys: NodeAPIKey[] }> {
  return apiGet<{ keys: NodeAPIKey[] }>(`/api/admin/nodes/${id}/keys`);
}

export function createNodeKey(
  id: string,
  input?: { scopes?: NodeScope[]; expires_in_days?: number; expires_at?: string },
): Promise<CreatedNodeKey> {
  return apiPostJson<CreatedNodeKey>(`/api/admin/nodes/${id}/keys`, input ?? {});
}

export function revokeNodeKey(
  nodeId: string,
  keyId: string,
): Promise<NodeAPIKey> {
  return apiDelete<NodeAPIKey>(`/api/admin/nodes/${nodeId}/keys/${keyId}`);
}

export function rotateNodeKey(
  nodeId: string,
  keyId: string,
  input?: {
    revoke_old?: boolean;
    expires_in_days?: number;
    expires_at?: string;
  },
): Promise<CreatedNodeKey> {
  return apiPostJson<CreatedNodeKey>(
    `/api/admin/nodes/${nodeId}/keys/${keyId}/rotate`,
    input ?? {},
  );
}
