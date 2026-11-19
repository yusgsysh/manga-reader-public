import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Loader } from "@cloudflare/kumo";
import { Power, Trash } from "@phosphor-icons/react";
import {
  deleteUser,
  fetchUsers,
  setUserStatus,
  updateUser,
  type AdminUser,
} from "../../api/admin";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";
import { useAuth } from "../../context/auth";

const QUERY_KEY = ["admin", "users"] as const;

const ROLE_LABELS: Record<string, string> = {
  admin: "管理员",
  user: "普通用户",
};

function displayName(user: AdminUser): string {
  return user.name || user.email || user.username;
}

function UserRow({ user }: { user: AdminUser }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();

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
            <span className="truncate font-medium">{displayName(user)}</span>
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
          <div className="mt-0.5 text-xs text-kumo-subtle">
            {user.email && user.email !== displayName(user)
              ? `${user.email} · `
              : ""}
            Kinde ID {user.user_id || "—"}
          </div>
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
    </div>
  );
}

export function UserManagementSection() {
  const usersQuery = useQuery({
    queryKey: QUERY_KEY,
    queryFn: fetchUsers,
    retry: false,
    staleTime: 0,
  });

  return (
    <Section
      title="用户管理"
      description="账号在首次登录时自动创建。这里只管理本地角色与启用状态；每个账号拥有独立的 Cookie、设置、书架和下载任务。"
    >
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
    </Section>
  );
}
