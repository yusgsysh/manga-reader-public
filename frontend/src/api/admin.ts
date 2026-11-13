import { apiGet, apiPatchJson, apiPostJson, apiPut } from "./client";

export type TenantStatus = "active" | "disabled";
export type UserRole = "platform_admin" | "tenant_admin" | "user";
export type UserStatus = "active" | "disabled";

export interface Tenant {
  id: number;
  name: string;
  slug: string;
  status: TenantStatus;
  created_at: string;
  updated_at: string;
}

export interface TenantUser {
  id: number;
  tenant_id: number;
  username: string;
  role: UserRole;
  status: UserStatus;
  created_at: string;
  updated_at: string;
}

export function fetchTenants(): Promise<{ tenants: Tenant[] }> {
  return apiGet<{ tenants: Tenant[] }>("/api/admin/tenants");
}

export interface CreateTenantInput {
  name: string;
  slug: string;
  admin_username?: string;
  admin_password?: string;
}

export function createTenant(input: CreateTenantInput): Promise<Tenant> {
  return apiPostJson<Tenant>("/api/admin/tenants", input);
}

export function updateTenant(
  id: number,
  input: { name?: string; slug?: string },
): Promise<Tenant> {
  return apiPut<Tenant>(`/api/admin/tenants/${id}`, input);
}

export function setTenantStatus(
  id: number,
  status: TenantStatus,
): Promise<Tenant> {
  return apiPatchJson<Tenant>(`/api/admin/tenants/${id}/status`, { status });
}

export function fetchTenantUsers(): Promise<{ users: TenantUser[] }> {
  return apiGet<{ users: TenantUser[] }>("/api/tenant/users");
}

export interface CreateTenantUserInput {
  username: string;
  password: string;
  role: Exclude<UserRole, "platform_admin">;
  status?: UserStatus;
}

export function createTenantUser(
  input: CreateTenantUserInput,
): Promise<TenantUser> {
  return apiPostJson<TenantUser>("/api/tenant/users", input);
}

export interface UpdateTenantUserInput {
  username?: string;
  password?: string;
  role?: Exclude<UserRole, "platform_admin">;
  status?: UserStatus;
}

export function updateTenantUser(
  id: number,
  input: UpdateTenantUserInput,
): Promise<TenantUser> {
  return apiPut<TenantUser>(`/api/tenant/users/${id}`, input);
}

export function setTenantUserStatus(
  id: number,
  status: UserStatus,
): Promise<TenantUser> {
  return apiPatchJson<TenantUser>(`/api/tenant/users/${id}/status`, { status });
}
