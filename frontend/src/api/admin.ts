import {
  apiDelete,
  apiGet,
  apiPatchJson,
  apiPostJson,
  apiPut,
} from "./client";

type UserStatus = "active" | "disabled";
export type UserRole = "admin" | "user";

export interface AdminUser {
  id: string;
  user_id: string;
  username: string;
  role: UserRole;
  status: UserStatus;
  created_at: string;
  updated_at: string;
}

export function fetchUsers(): Promise<{ users: AdminUser[] }> {
  return apiGet<{ users: AdminUser[] }>("/api/admin/users");
}

interface CreateUserInput {
  username: string;
  password: string;
  role?: UserRole;
  status?: UserStatus;
}

export function createUser(input: CreateUserInput): Promise<AdminUser> {
  return apiPostJson<AdminUser>("/api/admin/users", input);
}

interface UpdateUserInput {
  username?: string;
  password?: string;
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
