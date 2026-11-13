import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Loader } from "@cloudflare/kumo";
import { CircleNotch, PencilSimple, Plus, Power } from "@phosphor-icons/react";
import {
  createTenant,
  fetchTenants,
  setTenantStatus,
  updateTenant,
  type Tenant,
} from "../api/admin";
import { ErrorState } from "../components/common/ErrorState";
import { PageHeader, Section, useToast } from "../components/ui";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { useAuth } from "../context/auth";

const QUERY_KEY = ["admin", "tenants"] as const;

function TenantRow({ tenant }: { tenant: Tenant }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(tenant.name);
  const [slug, setSlug] = useState(tenant.slug);

  const save = useMutation({
    mutationFn: () => updateTenant(tenant.id, { name, slug }),
    onSuccess: () => {
      setEditing(false);
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("租户已更新");
    },
    onError: (error) => toast.error("更新租户失败", error),
  });

  const toggle = useMutation({
    mutationFn: () =>
      setTenantStatus(
        tenant.id,
        tenant.status === "active" ? "disabled" : "active",
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("租户状态已更新");
    },
    onError: (error) => toast.error("更新租户状态失败", error),
  });

  return (
    <div className="card-surface p-4">
      {editing ? (
        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <Input
              aria-label="租户名称"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <Input
              aria-label="租户 slug"
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
            />
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              onClick={() => save.mutate()}
              disabled={save.isPending}
            >
              {save.isPending && (
                <CircleNotch className="mr-1 size-3.5 animate-spin" />
              )}
              保存
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                setEditing(false);
                setName(tenant.name);
                setSlug(tenant.slug);
              }}
            >
              取消
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium">{tenant.name}</span>
              <span
                className={
                  tenant.status === "active"
                    ? "rounded-full bg-kumo-success/10 px-2 py-0.5 text-xs text-kumo-success"
                    : "rounded-full bg-kumo-danger/10 px-2 py-0.5 text-xs text-kumo-danger"
                }
              >
                {tenant.status === "active" ? "启用" : "禁用"}
              </span>
            </div>
            <div className="mt-0.5 font-mono text-xs text-kumo-subtle">
              {tenant.slug} · ID {tenant.id}
            </div>
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setEditing(true)}
            >
              <PencilSimple className="mr-1 size-3.5" />
              编辑
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => toggle.mutate()}
              disabled={toggle.isPending}
            >
              <Power className="mr-1 size-3.5" />
              {tenant.status === "active" ? "禁用" : "启用"}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

export function PlatformAdminPage() {
  useDocumentTitle("租户管理");
  const toast = useToast();
  const queryClient = useQueryClient();
  const { tenant: currentTenant } = useAuth();
  const tenantsQuery = useQuery({
    queryKey: QUERY_KEY,
    queryFn: fetchTenants,
    retry: false,
    staleTime: 0,
  });

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [adminUsername, setAdminUsername] = useState("admin");
  const [adminPassword, setAdminPassword] = useState("");

  const create = useMutation({
    mutationFn: () =>
      createTenant({
        name: name.trim(),
        slug: slug.trim(),
        admin_username: adminUsername.trim() || undefined,
        admin_password: adminPassword || undefined,
      }),
    onSuccess: () => {
      setName("");
      setSlug("");
      setAdminUsername("admin");
      setAdminPassword("");
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      toast.success("租户已创建");
    },
    onError: (error) => toast.error("创建租户失败", error),
  });

  const canCreate =
    name.trim() !== "" && slug.trim() !== "" && !create.isPending;

  return (
    <div className="space-y-6">
      <PageHeader title="租户管理" />
      <div>
        <h1 className="text-lg font-semibold tracking-tight">平台租户管理</h1>
        <p className="mt-1 text-sm text-kumo-subtle">
          当前登录租户：{currentTenant?.name ?? "-"}（{currentTenant?.slug ?? "-"}）
        </p>
      </div>

      <Section title="创建租户" description="可选地为新租户初始化一个租户管理员。">
        <div className="card-surface space-y-3 p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Input
              aria-label="租户名称"
              placeholder="租户名称"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <Input
              aria-label="租户 slug"
              placeholder="租户 slug，例如 team-a"
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
            />
            <Input
              aria-label="租户管理员用户名"
              placeholder="管理员用户名（可选）"
              value={adminUsername}
              onChange={(event) => setAdminUsername(event.target.value)}
            />
            <Input
              aria-label="租户管理员密码"
              type="password"
              placeholder="管理员密码（可选，至少 8 位）"
              value={adminPassword}
              onChange={(event) => setAdminPassword(event.target.value)}
            />
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
            创建租户
          </Button>
        </div>
      </Section>

      <Section title="租户列表">
        {tenantsQuery.isLoading ? (
          <div className="flex justify-center py-10">
            <Loader size={24} />
          </div>
        ) : tenantsQuery.isError ? (
          <ErrorState
            message="加载租户列表失败"
            onRetry={() => void tenantsQuery.refetch()}
          />
        ) : (
          <div className="space-y-3">
            {tenantsQuery.data?.tenants.map((tenant) => (
              <TenantRow key={tenant.id} tenant={tenant} />
            ))}
          </div>
        )}
      </Section>
    </div>
  );
}
