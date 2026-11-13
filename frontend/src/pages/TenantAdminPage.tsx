import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Loader } from "@cloudflare/kumo";
import { CircleNotch, Key, Plus, Power } from "@phosphor-icons/react";
import {
  createTenantUser,
  fetchTenantUsers,
  setTenantUserStatus,
  updateTenantUser,
  type TenantUser,
  type UserRole,
} from "../api/admin";
import { ErrorState } from "../components/common/ErrorState";
import { PageHeader, Section, useToast } from "../components/ui";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { useAuth } from "../context/auth";

const QUERY_KEY = ["tenant", "users"] as const;

const ROLE_LABELS: Record<string, string> = {
  platform_admin: "平台管理员",
  tenant_admin: "租户管理员",
  user: "普通用户",
};

function UserRow({ user }: { user: TenantUser }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const [resetOpen, setResetOpen] = useState(false);
  const [password, setPassword] = useState("");

  const toggle = useMutation({
    mutationFn: () =>
      setTenantUserStatus(
        user.id,
        user.status === "active" ? "disabled" : "active",
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("用户状态已更新");
    },
    onError: (error) => toast.error("更新用户状态失败", error),
  });

  const reset = useMutation({
    mutationFn: () => updateTenantUser(user.id, { password }),
    onSuccess: () => {
      setPassword("");
      setResetOpen(false);
      toast.success("密码已重置，旧会话已失效");
    },
    onError: (error) => toast.error("重置密码失败", error),
  });

  const isPlatformAdmin = user.role === "platform_admin";

  return (
    <div className="card-surface p-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium">{user.username}</span>
            {user.id === currentUser?.id && (
              <span className="rounded-full bg-kumo-tint px-2 py-0.5 text-xs text-kumo-subtle">
                当前用户
              </span>
            )}
            <span className="rounded-full bg-kumo-tint px-2 py-0.5 text-xs text-kumo-subtle">
              {ROLE_LABELS[user.role] ?? user.role}
            </span>
            <span
              className={
                user.status === "active"
                  ? "rounded-full bg-kumo-success/10 px-2 py-0.5 text-xs text-kumo-success"
                  : "rounded-full bg-kumo-danger/10 px-2 py-0.5 text-xs text-kumo-danger"
              }
            >
              {user.status === "active" ? "启用" : "禁用"}
            </span>
          </div>
          <div className="mt-0.5 text-xs text-kumo-subtle">ID {user.id}</div>
        </div>
        {!isPlatformAdmin && (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setResetOpen((open) => !open)}
            >
              <Key className="mr-1 size-3.5" />
              重置密码
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => toggle.mutate()}
              disabled={toggle.isPending}
            >
              <Power className="mr-1 size-3.5" />
              {user.status === "active" ? "禁用" : "启用"}
            </Button>
          </div>
        )}
      </div>

      {resetOpen && (
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <Input
            aria-label="新密码"
            type="password"
            placeholder="新密码，至少 8 位"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className="max-w-xs"
          />
          <Button
            size="sm"
            onClick={() => reset.mutate()}
            disabled={password.length < 8 || reset.isPending}
          >
            {reset.isPending && (
              <CircleNotch className="mr-1 size-3.5 animate-spin" />
            )}
            保存新密码
          </Button>
        </div>
      )}
    </div>
  );
}

export function TenantAdminPage() {
  useDocumentTitle("用户管理");
  const toast = useToast();
  const queryClient = useQueryClient();
  const { tenant } = useAuth();
  const usersQuery = useQuery({
    queryKey: QUERY_KEY,
    queryFn: fetchTenantUsers,
    retry: false,
    staleTime: 0,
  });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Exclude<UserRole, "platform_admin">>("user");

  const create = useMutation({
    mutationFn: () =>
      createTenantUser({
        username: username.trim(),
        password,
        role,
      }),
    onSuccess: () => {
      setUsername("");
      setPassword("");
      setRole("user");
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("用户已创建");
    },
    onError: (error) => toast.error("创建用户失败", error),
  });

  const canCreate =
    username.trim() !== "" && password.length >= 8 && !create.isPending;

  return (
    <div className="space-y-6">
      <PageHeader title="用户管理" />
      <div>
        <h1 className="text-lg font-semibold tracking-tight">租户用户管理</h1>
        <p className="mt-1 text-sm text-kumo-subtle">
          租户：{tenant?.name ?? "-"}（{tenant?.slug ?? "-"}）
        </p>
      </div>

      <Section title="创建用户">
        <div className="card-surface space-y-3 p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <Input
              aria-label="用户名"
              placeholder="用户名"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
            <Input
              aria-label="初始密码"
              type="password"
              placeholder="密码（至少 8 位）"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
            <select
              aria-label="角色"
              value={role}
              onChange={(event) =>
                setRole(event.target.value as Exclude<UserRole, "platform_admin">)
              }
              className="rounded-lg border border-kumo-hairline bg-kumo-base px-3 py-2 text-sm text-kumo-default"
            >
              <option value="user">普通用户</option>
              <option value="tenant_admin">租户管理员</option>
            </select>
          </div>
          <Button
            onClick={() => create.mutate()}
            disabled={!canCreate}
            size="sm"
          >
            {create.isPending ? (
              <CircleNotch className="mr-1 size-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1 size-3.5" />
            )}
            创建用户
          </Button>
        </div>
      </Section>

      <Section title="用户列表">
        {usersQuery.isLoading ? (
          <div className="flex justify-center py-10">
            <Loader size={24} />
          </div>
        ) : usersQuery.isError ? (
          <ErrorState
            message="加载用户列表失败"
            onRetry={() => void usersQuery.refetch()}
          />
        ) : (
          <div className="space-y-3">
            {usersQuery.data?.users.map((user) => (
              <UserRow key={user.id} user={user} />
            ))}
          </div>
        )}
      </Section>
    </div>
  );
}
