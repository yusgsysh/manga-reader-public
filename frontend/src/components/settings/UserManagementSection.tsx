import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Loader } from "@cloudflare/kumo";
import { CircleNotch, Key, Plus, Power, Trash } from "@phosphor-icons/react";
import {
  createUser,
  deleteUser,
  fetchUsers,
  setUserStatus,
  updateUser,
  type AdminUser,
  type UserRole,
} from "../../api/admin";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";
import { useAuth } from "../../context/auth";

const QUERY_KEY = ["admin", "users"] as const;

const ROLE_LABELS: Record<string, string> = {
  admin: "管理员",
  user: "普通用户",
};

function UserRow({ user }: { user: AdminUser }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const [resetOpen, setResetOpen] = useState(false);
  const [password, setPassword] = useState("");

  const toggle = useMutation({
    mutationFn: () =>
      setUserStatus(user.id, user.status === "active" ? "disabled" : "active"),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("用户状态已更新");
    },
    onError: (error) => toast.error("更新用户状态失败", error),
  });

  const roleMutation = useMutation({
    mutationFn: () =>
      updateUser(user.id, {
        role: user.role === "admin" ? "user" : "admin",
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("用户角色已更新");
    },
    onError: (error) => toast.error("更新用户角色失败", error),
  });

  const reset = useMutation({
    mutationFn: () => updateUser(user.id, { password }),
    onSuccess: () => {
      setPassword("");
      setResetOpen(false);
      toast.success("密码已重置，旧会话已失效");
    },
    onError: (error) => toast.error("重置密码失败", error),
  });

  const remove = useMutation({
    mutationFn: () => deleteUser(user.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("用户及其个人数据已删除");
    },
    onError: (error) => toast.error("删除用户失败", error),
  });

  const isSelf = user.id === currentUser?.id;

  return (
    <div className="card-surface p-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium">{user.username}</span>
            {isSelf && (
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
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => roleMutation.mutate()}
            disabled={roleMutation.isPending}
          >
            {user.role === "admin" ? "降为普通用户" : "设为管理员"}
          </Button>
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
            disabled={toggle.isPending || isSelf}
          >
            <Power className="mr-1 size-3.5" />
            {user.status === "active" ? "禁用" : "启用"}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => remove.mutate()}
            disabled={remove.isPending || isSelf}
          >
            <Trash className="mr-1 size-3.5" />
            删除
          </Button>
        </div>
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

export function UserManagementSection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const usersQuery = useQuery({
    queryKey: QUERY_KEY,
    queryFn: fetchUsers,
    retry: false,
    staleTime: 0,
  });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<UserRole>("user");

  const create = useMutation({
    mutationFn: () =>
      createUser({ username: username.trim(), password, role }),
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
    <Section
      title="用户管理"
      description="每个账号拥有独立的 ExHentai Cookie、个人设置、书架、阅读记录和下载任务；数据不会跨账号共享。"
    >
      <div className="space-y-4">
        <div className="card-surface space-y-3 p-4">
          <h3 className="text-xs font-medium text-kumo-subtle">创建用户</h3>
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
              onChange={(event) => setRole(event.target.value as UserRole)}
              className="rounded-lg border border-kumo-hairline bg-kumo-base px-3 py-2 text-sm text-kumo-default"
            >
              <option value="user">普通用户</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <Button onClick={() => create.mutate()} disabled={!canCreate} size="sm">
            {create.isPending ? (
              <CircleNotch className="mr-1 size-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1 size-3.5" />
            )}
            创建用户
          </Button>
        </div>

        <div className="space-y-3">
          <h3 className="text-xs font-medium text-kumo-subtle">用户列表</h3>
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
            usersQuery.data?.users.map((user) => (
              <UserRow key={user.id} user={user} />
            ))
          )}
        </div>
      </div>
    </Section>
  );
}
