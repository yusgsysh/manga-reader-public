import { apiDelete, apiGet, apiPatchJson, apiPut } from "./client";

type UserStatus = "active" | "disabled";
export type UserRole = "admin" | "user";

export interface AdminUser {
  /** Local user id (UUID). */
  id: string;
  /** Verified Kinde subject. */
  user_id: string;
  username: string;
  email: string;
  name: string;
  avatar_url: string | null;
  role: UserRole;
  status: UserStatus;
  created_at: string;
  updated_at: string;
}

export function fetchUsers(): Promise<{ users: AdminUser[] }> {
  return apiGet<{ users: AdminUser[] }>("/api/admin/users");
}

interface UpdateUserInput {
  username?: string;
  role?: UserRole;
  status?: UserStatus;
}

export function updateUser(
  id: string,
  input: UpdateUserInput,
): Promise<AdminUser> {
  return apiPut<AdminUser>(`/api/admin/users/${id}`, input);
}

export function setUserStatus(
  id: string,
  status: UserStatus,
): Promise<AdminUser> {
  return apiPatchJson<AdminUser>(`/api/admin/users/${id}/status`, { status });
}

export function deleteUser(id: string): Promise<{ deleted: boolean }> {
  return apiDelete<{ deleted: boolean }>(`/api/admin/users/${id}`);
}
