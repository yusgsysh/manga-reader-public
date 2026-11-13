import { apiDelete, apiGet, apiPostJson, apiPut } from "./client";

export type NodeStatus = "active" | "disabled";
export type NodeKeyStatus = "active" | "revoked";
export const NODE_SCOPES = ["sync:read", "sync:write", "sync:events"] as const;
export type NodeScope = (typeof NODE_SCOPES)[number];

export interface SyncNode {
  id: number;
  name: string;
  tenant_id: number;
  status: NodeStatus;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface NodeAPIKey {
  id: number;
  node_id: number;
  key_prefix: string;
  scopes: NodeScope[];
  status: NodeKeyStatus;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
  revoked_at: string | null;
}

export interface CreatedNodeKey {
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
  tenant_id: number;
  status?: NodeStatus;
}): Promise<SyncNode> {
  return apiPostJson<SyncNode>("/api/admin/nodes", input);
}

export function updateNode(
  id: number,
  input: { name?: string; status?: NodeStatus },
): Promise<SyncNode> {
  return apiPut<SyncNode>(`/api/admin/nodes/${id}`, input);
}

export function deleteNode(id: number): Promise<{ deleted: boolean }> {
  return apiDelete<{ deleted: boolean }>(`/api/admin/nodes/${id}`);
}

export function fetchNodeKeys(
  id: number,
): Promise<{ keys: NodeAPIKey[] }> {
  return apiGet<{ keys: NodeAPIKey[] }>(`/api/admin/nodes/${id}/keys`);
}

export function createNodeKey(
  id: number,
  input?: { scopes?: NodeScope[]; expires_at?: string },
): Promise<CreatedNodeKey> {
  return apiPostJson<CreatedNodeKey>(`/api/admin/nodes/${id}/keys`, input ?? {});
}

export function revokeNodeKey(
  nodeId: number,
  keyId: number,
): Promise<NodeAPIKey> {
  return apiDelete<NodeAPIKey>(`/api/admin/nodes/${nodeId}/keys/${keyId}`);
}

export function rotateNodeKey(
  nodeId: number,
  keyId: number,
  input?: { revoke_old?: boolean; expires_at?: string },
): Promise<CreatedNodeKey> {
  return apiPostJson<CreatedNodeKey>(
    `/api/admin/nodes/${nodeId}/keys/${keyId}/rotate`,
    input ?? {},
  );
}
