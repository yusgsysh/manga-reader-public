import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Button,
  DatePicker,
  Input,
  Loader,
  Popover,
  Select,
} from "@cloudflare/kumo";
import {
  CalendarBlank,
  CircleNotch,
  Plus,
  Trash,
} from "@phosphor-icons/react";
import {
  createUserSyncKey,
  fetchSyncStatus,
  fetchUserSyncKeys,
  revokeUserSyncKey,
  type CreateUserSyncKeyInput,
  type UserSyncKey,
} from "../../api/sync";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";
import { formatDate, formatDateTime } from "../../lib/time";

const KEYS_KEY = ["sync-keys"] as const;

/** Default to 30 days, the recommended rotation window. */
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

export function ApiKeySection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const statusQuery = useQuery({
    queryKey: ["sync-status"],
    queryFn: fetchSyncStatus,
    retry: false,
    staleTime: 0,
  });
  const hostEnabled = statusQuery.data?.host_enabled === true;
  const keysQuery = useQuery({
    queryKey: KEYS_KEY,
    queryFn: fetchUserSyncKeys,
    enabled: hostEnabled,
    retry: false,
    staleTime: 0,
  });

  const [name, setName] = useState("");
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [expiry, setExpiry] = useState("30");
  const [customDate, setCustomDate] = useState<Date>();
  const [calendarOpen, setCalendarOpen] = useState(false);
  // Snapshot the day once so date bounds do not shift mid-render.
  const [today] = useState(() => startOfDay(new Date()));

  const tomorrow = addDays(today, 1);
  const maxExpiryDate = addDays(today, 3650);
  const customExpiry = customDate ? endOfDay(customDate) : undefined;
  const customValid =
    customDate !== undefined &&
    startOfDay(customDate).getTime() >= tomorrow.getTime() &&
    startOfDay(customDate).getTime() <= maxExpiryDate.getTime();
  const presetDays =
    expiry === "never" || expiry === "custom" ? undefined : Number(expiry);
  const validExpiry =
    expiry === "never" || (expiry === "custom" ? customValid : true);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: KEYS_KEY });
  };

  const create = useMutation({
    mutationFn: () => {
      const input: CreateUserSyncKeyInput = {};
      const trimmed = name.trim();
      if (trimmed !== "") input.name = trimmed;
      if (expiry === "custom") {
        if (customExpiry) input.expires_at = customExpiry.toISOString();
      } else if (presetDays !== undefined) {
        input.expires_in_days = presetDays;
      }
      return createUserSyncKey(input);
    },
    onSuccess: (result) => {
      setPlaintext(result.key);
      setName("");
      setExpiry("30");
      setCustomDate(undefined);
      setCalendarOpen(false);
      refresh();
      toast.success("API Key 已创建", "完整密钥只显示这一次");
    },
    onError: (error) => toast.error("创建 API Key 失败", error),
  });

  const revoke = useMutation({
    mutationFn: (key: UserSyncKey) => revokeUserSyncKey(key.id),
    onSuccess: () => {
      refresh();
      toast.success("API Key 已撤销");
    },
    onError: (error) => toast.error("撤销 API Key 失败", error),
  });

  return (
    <Section
      title="API Key"
      description="为当前账号创建同步 Key，可设置名称与有效期；一把 Key 只能同步一个账号。"
    >
      {statusQuery.isLoading ? (
        <div className="card-surface flex items-center gap-2 p-4 text-sm text-kumo-subtle">
          <Loader size={16} />
          正在读取…
        </div>
      ) : !hostEnabled ? (
        <p className="card-surface p-4 text-sm text-kumo-subtle">
          本实例未启用同步宿主（`MANGA_READER_SYNC_ENABLED=true`），无法创建同步 Key。
        </p>
      ) : (
        <div className="space-y-4">
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
                <div className="text-xs font-medium text-kumo-subtle">名称</div>
                <Input
                  aria-label="API Key 名称"
                  placeholder="例如 desktop-home"
                  maxLength={100}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
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
                        <CalendarBlank
                          className="size-4 shrink-0"
                          weight="bold"
                        />
                        <span className="min-w-0 truncate">
                          {customDate ? formatDate(customDate) : "选择日期"}
                        </span>
                      </Popover.Trigger>
                      <Popover.Content
                        side="bottom"
                        align="start"
                        className="p-2"
                      >
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
                disabled={create.isPending || !validExpiry}
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
                  <span className="font-medium">
                    {key.name || "未命名"}
                  </span>
                  <code className="font-mono text-kumo-subtle">
                    {key.key_prefix}…
                  </code>
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
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => revoke.mutate(key)}
                      disabled={revoke.isPending}
                    >
                      <Trash className="mr-1 size-3.5" />
                      撤销
                    </Button>
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
      )}
    </Section>
  );
}
