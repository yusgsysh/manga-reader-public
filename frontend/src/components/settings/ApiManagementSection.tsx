import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Checkbox, DatePicker, Input, Loader, Popover, Select } from "@cloudflare/kumo";
import {
  ArrowsClockwise,
  CalendarBlank,
  CircleNotch,
  Key,
  Plus,
  Trash,
} from "@phosphor-icons/react";
import {
  METRICS_SCOPE,
  NODE_SCOPES,
  createNode,
  createNodeKey,
  deleteNode,
  fetchNodeKeys,
  fetchNodes,
  revokeNodeKey,
  rotateNodeKey,
  updateNode,
  type NodeAPIKey,
  type NodeScope,
  type SyncNode,
} from "../../api/nodes";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";
import { formatDate, formatDateTime } from "../../lib/time";

const NODES_KEY = ["admin", "nodes"] as const;

/**
 * GitHub-style lifetime presets. A key defaults to 30 days, the recommended
 * rotation window, instead of quietly living forever.
 */
const EXPIRY_PRESETS: ReadonlyArray<{ value: string; label: string }> = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天（推荐）" },
  { value: "60", label: "60 天" },
  { value: "90", label: "90 天" },
  { value: "180", label: "180 天" },
  { value: "365", label: "1 年" },
  { value: "custom", label: "自定义…" },
  { value: "never", label: "永不过期" },
];

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function endOfDay(date: Date): Date {
  return new Date(
    date.getFullYear(),
    date.getMonth(),
    date.getDate(),
    23,
    59,
    59,
    999,
  );
}

function KeyList({ node }: { node: SyncNode }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [scopes, setScopes] = useState<NodeScope[]>([...NODE_SCOPES]);
  const [metricsAccess, setMetricsAccess] = useState(false);
  const [expiry, setExpiry] = useState("30");
  const [customDate, setCustomDate] = useState<Date>();
  const [calendarOpen, setCalendarOpen] = useState(false);
  // Snapshot the day once so date bounds do not shift mid-render.
  const [today] = useState(() => startOfDay(new Date()));

  const tomorrow = addDays(today, 1);
  const maxExpiryDate = addDays(today, 3650);
  // A custom calendar date expires at the end of that local day.
  const customExpiry = customDate ? endOfDay(customDate) : undefined;
  const customValid =
    customDate !== undefined &&
    startOfDay(customDate).getTime() >= tomorrow.getTime() &&
    startOfDay(customDate).getTime() <= maxExpiryDate.getTime();

  const presetDays =
    expiry === "never" || expiry === "custom" ? undefined : Number(expiry);
  const validExpiry =
    expiry === "never" || (expiry === "custom" ? customValid : true);
  const selectedScopes: NodeScope[] = metricsAccess
    ? [...scopes, METRICS_SCOPE]
    : scopes;

  const keysQuery = useQuery({
    queryKey: ["admin", "node-keys", node.id],
    queryFn: () => fetchNodeKeys(node.id),
    retry: false,
    staleTime: 0,
  });

  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ["admin", "node-keys", node.id],
    });
  };

  const create = useMutation({
    mutationFn: () => {
      const input: {
        scopes: NodeScope[];
        expires_in_days?: number;
        expires_at?: string;
      } = { scopes: selectedScopes };
      if (expiry === "custom") {
        if (customExpiry) input.expires_at = customExpiry.toISOString();
      } else if (presetDays !== undefined) {
        input.expires_in_days = presetDays;
      }
      return createNodeKey(node.id, input);
    },
    onSuccess: (result) => {
      setPlaintext(result.key);
      setExpiry("30");
      setCustomDate(undefined);
      setCalendarOpen(false);
      refresh();
      toast.success("API Key 已创建", "完整密钥只显示这一次");
    },
    onError: (error) => toast.error("创建 API Key 失败", error),
  });

  const rotate = useMutation({
    mutationFn: (key: NodeAPIKey) =>
      rotateNodeKey(node.id, key.id, { revoke_old: false }),
    onSuccess: (result) => {
      setPlaintext(result.key);
      refresh();
      toast.success("API Key 已轮换", "旧 Key 仍然有效，确认后可撤销");
    },
    onError: (error) => toast.error("轮换 API Key 失败", error),
  });

  const revoke = useMutation({
    mutationFn: (key: NodeAPIKey) => revokeNodeKey(node.id, key.id),
    onSuccess: () => {
      refresh();
      toast.success("API Key 已撤销");
    },
    onError: (error) => toast.error("撤销 API Key 失败", error),
  });

  const toggleScope = (scope: NodeScope) => {
    setScopes((current) =>
      current.includes(scope)
        ? current.filter((item) => item !== scope)
        : [...current, scope],
    );
  };

  return (
    <div className="mt-4 space-y-4 border-t border-kumo-hairline pt-4">
      {plaintext && (
        <div className="rounded-xl border border-kumo-success/30 bg-kumo-success/10 p-3">
          <div className="text-xs font-medium text-kumo-success">
            完整 API Key（只显示一次）
          </div>
          <code className="mt-2 block break-all rounded-lg bg-kumo-base p-2 text-xs">
            {plaintext}
          </code>
          <Button
            size="sm"
            variant="secondary"
            className="mt-2"
            onClick={() => {
              void navigator.clipboard?.writeText(plaintext);
              toast.info("已复制到剪贴板");
            }}
          >
            复制
          </Button>
        </div>
      )}

      <div className="card-surface space-y-4 p-4">
        <h3 className="text-xs font-medium text-kumo-subtle">创建 API Key</h3>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <div className="text-xs font-medium text-kumo-subtle">权限范围</div>
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {NODE_SCOPES.map((scope) => (
                <Checkbox
                  key={scope}
                  label={scope}
                  checked={scopes.includes(scope)}
                  onCheckedChange={() => toggleScope(scope)}
                />
              ))}
            </div>
            <Checkbox
              label={METRICS_SCOPE}
              checked={metricsAccess}
              onCheckedChange={(checked) => setMetricsAccess(Boolean(checked))}
            />
          </div>

          <div className="space-y-2">
            <div className="text-xs font-medium text-kumo-subtle">有效期</div>
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={expiry}
                onValueChange={(next) => {
                  const value = next ?? "30";
                  setExpiry(value);
                  if (value === "custom") setCalendarOpen(true);
                }}
                aria-label="有效期"
                items={EXPIRY_PRESETS}
                className="w-40"
              />
              {expiry === "custom" && (
                <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
                  <Popover.Trigger
                    render={
                      <Button
                        variant="secondary"
                        size="sm"
                        className="justify-start"
                      />
                    }
                  >
                    <CalendarBlank className="size-4 shrink-0" weight="bold" />
                    <span className="min-w-0 truncate">
                      {customDate ? formatDate(customDate) : "选择日期"}
                    </span>
                  </Popover.Trigger>
                  <Popover.Content side="bottom" align="start" className="p-2">
                    <DatePicker
                      mode="single"
                      selected={customDate}
                      onChange={(date) => {
                        if (!date) return;
                        setCustomDate(date);
                        setCalendarOpen(false);
                      }}
                      disabled={{ before: tomorrow, after: maxExpiryDate }}
                      startMonth={today}
                      endMonth={maxExpiryDate}
                    />
                  </Popover.Content>
                </Popover>
              )}
            </div>
            <p className="text-xs text-kumo-subtle">
              {expiry === "never"
                ? "Key 不会自动过期，建议定期轮换。"
                : expiry === "custom"
                  ? customValid && customExpiry
                    ? `将于 ${formatDateTime(customExpiry)} 到期。`
                    : "请选择到期日期（至少 1 天后，最长 10 年）。"
                  : "到期后 Key 自动失效。"}
            </p>
          </div>
        </div>

        <div className="flex justify-end">
          <Button
            size="sm"
            onClick={() => create.mutate()}
            disabled={
              create.isPending || selectedScopes.length === 0 || !validExpiry
            }
          >
            {create.isPending ? (
              <CircleNotch className="mr-1 size-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1 size-3.5" />
            )}
            创建 Key
          </Button>
        </div>
      </div>

      <div className="space-y-2">
        <h3 className="text-xs font-medium text-kumo-subtle">已有 Key</h3>
        {keysQuery.isLoading ? (
          <div className="flex justify-center py-4">
            <Loader size={20} />
          </div>
        ) : keysQuery.isError ? (
          <ErrorState
            message="加载 API Key 失败"
            onRetry={() => void keysQuery.refetch()}
          />
        ) : keysQuery.data?.keys.length ? (
          keysQuery.data.keys.map((key) => (
            <div
              key={key.id}
              className="flex flex-wrap items-center gap-2 rounded-lg bg-kumo-recessed p-2.5 text-xs"
            >
              <code className="font-mono">{key.key_prefix}…</code>
              <span className="rounded-full bg-kumo-tint px-2 py-0.5 text-[11px] text-kumo-subtle">
                {key.scopes.join(", ")}
              </span>
              <span
                className={
                  key.status === "active"
                    ? "rounded-full bg-kumo-success/10 px-2 py-0.5 text-kumo-success"
                    : "rounded-full bg-kumo-danger/10 px-2 py-0.5 text-kumo-danger"
                }
              >
                {key.status === "active" ? "有效" : "已撤销"}
              </span>
              <span className="text-kumo-subtle">
                {key.expires_at
                  ? `过期 ${formatDateTime(key.expires_at)}`
                  : "永不过期"}
              </span>
              {key.last_used_at && (
                <span className="text-kumo-subtle">
                  最后使用 {formatDateTime(key.last_used_at)}
                </span>
              )}
              <span className="flex-1" />
              {key.status === "active" && (
                <>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => rotate.mutate(key)}
                    disabled={rotate.isPending}
                  >
                    <ArrowsClockwise className="mr-1 size-3.5" />
                    轮换
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => revoke.mutate(key)}
                    disabled={revoke.isPending}
                  >
                    <Trash className="mr-1 size-3.5" />
                    撤销
                  </Button>
                </>
              )}
            </div>
          ))
        ) : (
          <p className="rounded-lg bg-kumo-recessed p-3 text-xs text-kumo-subtle">
            暂无 API Key。创建后完整密钥只显示一次。
          </p>
        )}
      </div>
    </div>
  );
}

function NodeRow({ node }: { node: SyncNode }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [name, setName] = useState(node.name);
  const [editing, setEditing] = useState(false);
  const [keysOpen, setKeysOpen] = useState(false);

  const save = useMutation({
    mutationFn: () => updateNode(node.id, { name }),
    onSuccess: () => {
      setEditing(false);
      void queryClient.invalidateQueries({ queryKey: NODES_KEY });
      toast.success("节点已更新");
    },
    onError: (error) => toast.error("更新节点失败", error),
  });

  const toggle = useMutation({
    mutationFn: () =>
      updateNode(node.id, {
        status: node.status === "active" ? "disabled" : "active",
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: NODES_KEY });
      toast.success("节点状态已更新");
    },
    onError: (error) => toast.error("更新节点状态失败", error),
  });

  const remove = useMutation({
    mutationFn: () => deleteNode(node.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: NODES_KEY });
      toast.success("节点及其 API Key 已删除");
    },
    onError: (error) => toast.error("删除节点失败", error),
  });

  return (
    <div className="card-surface p-4">
      <div className="flex flex-wrap items-center gap-3">
        {editing ? (
          <div className="flex flex-1 gap-2">
            <Input
              aria-label="节点名称"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
              保存
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                setEditing(false);
                setName(node.name);
              }}
            >
              取消
            </Button>
          </div>
        ) : (
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{node.name}</span>
              <span
                className={
                  node.status === "active"
                    ? "rounded-full bg-kumo-success/10 px-2 py-0.5 text-xs text-kumo-success"
                    : "rounded-full bg-kumo-danger/10 px-2 py-0.5 text-xs text-kumo-danger"
                }
              >
                {node.status}
              </span>
            </div>
            <div className="mt-0.5 text-xs text-kumo-subtle">
              ID {node.id}
              {node.last_seen_at
                ? ` · 最后在线 ${formatDateTime(node.last_seen_at)}`
                : " · 尚未连接"}
            </div>
          </div>
        )}
        {!editing && (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="secondary" onClick={() => setEditing(true)}>
              编辑
            </Button>
            <Button size="sm" variant="secondary" onClick={() => toggle.mutate()}>
              {node.status === "active" ? "禁用" : "启用"}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setKeysOpen((open) => !open)}
            >
              <Key className="mr-1 size-3.5" />
              API Keys
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => remove.mutate()}
              disabled={remove.isPending}
            >
              <Trash className="mr-1 size-3.5" />
              删除
            </Button>
          </div>
        )}
      </div>
      {keysOpen && <KeyList node={node} />}
    </div>
  );
}

export function ApiManagementSection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const nodesQuery = useQuery({
    queryKey: NODES_KEY,
    queryFn: fetchNodes,
    retry: false,
    staleTime: 0,
  });
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => createNode({ name: name.trim() }),
    onSuccess: () => {
      setName("");
      void queryClient.invalidateQueries({ queryKey: NODES_KEY });
      toast.success("节点已创建");
    },
    onError: (error) => toast.error("创建节点失败", error),
  });

  return (
    <Section
      title="API Key"
      description="管理节点 API Key（每个节点可有多把 Key）。sync:write 用于同步推送、sync:events 用于事件流；metrics:read 可读取 /metrics。与用户 JWT 独立。"
    >
      <div className="space-y-4">
        <div className="card-surface space-y-3 p-4">
          <h3 className="text-xs font-medium text-kumo-subtle">创建节点</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <Input
              aria-label="节点名称"
              placeholder="节点名称，例如 desktop-home"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <Button
            size="sm"
            onClick={() => create.mutate()}
            disabled={!name.trim() || create.isPending}
          >
            {create.isPending ? (
              <CircleNotch className="mr-1 size-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1 size-3.5" />
            )}
            创建节点
          </Button>
        </div>

        <div className="space-y-3">
          <h3 className="text-xs font-medium text-kumo-subtle">节点列表</h3>
          {nodesQuery.isLoading ? (
            <div className="flex justify-center py-10">
              <Loader size={24} />
            </div>
          ) : nodesQuery.isError ? (
            <ErrorState
              message="加载节点列表失败"
              onRetry={() => void nodesQuery.refetch()}
            />
          ) : (
            nodesQuery.data?.nodes.map((node) => (
              <NodeRow key={node.id} node={node} />
            ))
          )}
        </div>
      </div>
    </Section>
  );
}
