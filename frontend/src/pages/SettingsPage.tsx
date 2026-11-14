import { useMemo, useState, type ReactNode } from "react";
import {
  ArrowsClockwise,
  CaretLeft,
  CaretRight,
  CircleNotch,
  Cookie,
  Monitor,
  Moon,
  Palette,
  Sun,
} from "@phosphor-icons/react";
import { Button, Dialog, Input, Switch, cn } from "@cloudflare/kumo";
import { HexColorPicker, HexColorInput } from "react-colorful";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchUpstreamDown, setUpstreamDown } from "../api/dev";
import {
  SYNC_KEY_MASK,
  fetchSyncConfig,
  fetchSyncStatus,
  saveSyncConfig,
  startFullSync,
  type SyncConfig,
  type SyncConfigUpdate,
} from "../api/sync";
import { ApiRequestError } from "../api/client";
import {
  clearCookie,
  fetchCookie,
  saveCookie,
  verifyCookie,
  type CookieSummary,
  type ExhentaiSite,
} from "../api/settings";
import { useTheme } from "../hooks/useTheme";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { SETTINGS_STALE_TIME } from "../lib/cacheConfig";
import { ACCENT_PRESETS, DEFAULT_ACCENT, isPresetAccent } from "../lib/accent";
import { formatDateTime } from "../lib/time";
import type { ThemeMode } from "../lib/theme";
import { PageHeader, Section, useToast } from "../components/ui";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { useAuth } from "../context/auth";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { TagRulesSection } from "../components/settings/TagRulesSection";
import { UserManagementSection } from "../components/settings/UserManagementSection";
import { ApiManagementSection } from "../components/settings/ApiManagementSection";
import { SettingsSidebar } from "../components/settings/SettingsSidebar";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

const CUSTOM_ACCENT_SEED = "#6366f1";

const COOKIE_STATUS_LABELS: Record<CookieSummary["status"], string> = {
  unset: "未配置",
  unverified: "待验证",
  valid: "有效",
  invalid: "无效或已过期",
};

interface CookieField {
  id: "ipb_member_id" | "ipb_pass_hash" | "igneous" | "sk";
  label: string;
  required: boolean;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}

function CookieSection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["settings", "cookie"],
    queryFn: fetchCookie,
    retry: false,
    staleTime: 0,
  });
  const [ipbMemberId, setIpbMemberId] = useState("");
  const [ipbPassHash, setIpbPassHash] = useState("");
  const [igneous, setIgneous] = useState("");
  const [sk, setSk] = useState("");
  const [site, setSite] = useState<ExhentaiSite>("exhentai");
  const [applied, setApplied] = useState<CookieSummary | null>(null);
  const current = query.data;
  if (current && current !== applied) {
    setApplied(current);
    setSite(current.site);
  }

  const configured = current?.configured ?? false;
  const hasMemberId = ipbMemberId.trim() !== "";
  const hasPassHash = ipbPassHash.trim() !== "";
  const credentialsComplete = hasMemberId && hasPassHash;
  const credentialsPartial = hasMemberId !== hasPassHash;
  const hasOptional = igneous.trim() !== "" || sk.trim() !== "";
  const siteChanged = site !== (current?.site ?? "exhentai");

  const save = useMutation({
    mutationFn: () =>
      saveCookie({
        ipb_member_id: ipbMemberId.trim(),
        ipb_pass_hash: ipbPassHash.trim(),
        igneous: igneous.trim(),
        sk: sk.trim(),
        site,
      }),
    onSuccess: (data) => {
      queryClient.setQueryData(["settings", "cookie"], data);
      setIpbMemberId("");
      setIpbPassHash("");
      setIgneous("");
      setSk("");
      toast.success("ExHentai Cookie 已保存");
    },
    onError: (error) => toast.error("保存 Cookie 失败", error),
  });

  const verify = useMutation({
    mutationFn: verifyCookie,
    onSuccess: (data) => {
      queryClient.setQueryData(["settings", "cookie"], data);
      toast.show({
        title:
          data.status === "valid"
            ? "Cookie 验证通过"
            : "Cookie 验证失败，请重新复制",
        variant: data.status === "valid" ? "success" : "info",
      });
    },
    onError: (error) => toast.error("验证 Cookie 失败", error),
  });

  const clear = useMutation({
    mutationFn: clearCookie,
    onSuccess: (data) => {
      queryClient.setQueryData(["settings", "cookie"], data);
      setIpbMemberId("");
      setIpbPassHash("");
      setIgneous("");
      setSk("");
      toast.success("已清除 ExHentai Cookie");
    },
    onError: (error) => toast.error("清除 Cookie 失败", error),
  });

  const summary = current;
  const canSave =
    !save.isPending &&
    !credentialsPartial &&
    (siteChanged || credentialsComplete || hasOptional) &&
    (configured || credentialsComplete);

  const fields: CookieField[] = [
    {
      id: "ipb_member_id",
      label: "ipb_member_id",
      required: true,
      value: ipbMemberId,
      onChange: setIpbMemberId,
      placeholder: "例如 12345678",
    },
    {
      id: "ipb_pass_hash",
      label: "ipb_pass_hash",
      required: true,
      value: ipbPassHash,
      onChange: setIpbPassHash,
      placeholder: "32 位哈希",
    },
    {
      id: "igneous",
      label: "igneous",
      required: false,
      value: igneous,
      onChange: setIgneous,
      placeholder: "可选，例如 a1b2c3d4e5",
    },
    {
      id: "sk",
      label: "sk",
      required: false,
      value: sk,
      onChange: setSk,
      placeholder: "可选，部分账号需要",
    },
  ];

  return (
    <Section title="账号">
      <div className="card-surface space-y-4 p-4 text-sm">
        {query.isLoading ? (
          <div className="flex items-center gap-2 text-kumo-subtle">
            <CircleNotch className="size-4 animate-spin" />
            正在读取…
          </div>
        ) : query.isError ? (
          <p className="text-kumo-subtle">无法读取 Cookie 状态。</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Cookie className="size-5 text-kumo-subtle" />
              <span className="font-medium">
                {configured ? COOKIE_STATUS_LABELS[summary?.status ?? "unset"] : "未配置"}
              </span>
              {configured && (
                <span className="text-xs text-kumo-subtle">
                  igneous {summary?.has_igneous ? "已提供" : "缺失"} · sk{" "}
                  {summary?.has_sk ? "已提供" : "缺失"}
                </span>
              )}
            </div>
            <p className="text-xs text-kumo-subtle">
              只属于当前账号。服务端加密保存，不会回传到浏览器，也不会被其他用户使用。
            </p>

            <div>
              <div className="mb-1.5 text-sm font-medium">ExHentai 站点</div>
              <select
                aria-label="ExHentai 站点"
                value={site}
                onChange={(event) => setSite(event.target.value as ExhentaiSite)}
                className="h-9 w-full rounded-lg border border-kumo-hairline bg-kumo-base px-3 text-sm text-kumo-default"
              >
                <option value="exhentai">ExHentai (exhentai.org)</option>
                <option value="e-hentai">E-Hentai (e-hentai.org)</option>
              </select>
            </div>

            <div className="space-y-3">
              {fields.map((field) => (
                <div key={field.id}>
                  <div className="mb-1.5 flex items-baseline gap-2">
                    <label
                      htmlFor={`cookie-${field.id}`}
                      className="font-mono text-xs font-medium text-kumo-default"
                    >
                      {field.label}
                    </label>
                    <span className="text-xs text-kumo-subtle">
                      {field.required ? "必填" : "可选"}
                    </span>
                  </div>
                  <Input
                    id={`cookie-${field.id}`}
                    aria-label={field.label}
                    type="text"
                    spellCheck={false}
                    autoComplete="off"
                    placeholder={field.placeholder}
                    value={field.value}
                    onChange={(event) => field.onChange(event.target.value)}
                    disabled={save.isPending}
                    className="font-mono text-xs"
                  />
                </div>
              ))}
              {configured && (
                <p className="text-xs text-kumo-subtle">
                  已保存；留空保持不变，输入新值可替换对应字段。
                </p>
              )}
              {credentialsPartial && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  ipb_member_id 与 ipb_pass_hash 需要同时填写。
                </p>
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              <Button size="sm" onClick={() => save.mutate()} disabled={!canSave}>
                {save.isPending && (
                  <CircleNotch className="mr-1 size-4 animate-spin" />
                )}
                保存
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => verify.mutate()}
                disabled={!configured || verify.isPending}
              >
                {verify.isPending && (
                  <CircleNotch className="mr-1 size-4 animate-spin" />
                )}
                验证
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => clear.mutate()}
                disabled={!configured || clear.isPending}
              >
                清除
              </Button>
            </div>
          </>
        )}
      </div>
    </Section>
  );
}

const CUSTOM_SWATCH_GRADIENT =
  "conic-gradient(from 0deg, #ef4444, #eab308, #22c55e, #06b6d4, #3b82f6, #a855f7, #ef4444)";

function AccentSwatch({
  label,
  color,
  active,
  onClick,
}: {
  label: string;
  color: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "size-9 rounded-full border border-kumo-hairline transition-transform hover:scale-105",
        active &&
          "ring-2 ring-[var(--app-accent)] ring-offset-2 ring-offset-[var(--color-kumo-base)]",
      )}
      style={{ backgroundColor: color }}
    />
  );
}

function AppearanceSection() {
  const { mode, setMode, accent, setAccent } = useTheme();
  const [showCustom, setShowCustom] = useState(false);

  const isCustom = accent !== null && !isPresetAccent(accent);
  const showPicker = showCustom && isCustom && accent !== null;

  const handleSelectCustom = () => {
    setShowCustom(true);
    if (!isCustom) setAccent(CUSTOM_ACCENT_SEED);
  };

  return (
    <Section title="外观">
      <div className="space-y-6">
        <div>
          <div className="mb-2 text-sm font-medium">主题模式</div>
          <div className="flex flex-wrap gap-2">
            {THEME_OPTIONS.map((option) => {
              const Icon = option.icon;
              const active = mode === option.mode;
              return (
                <button
                  key={option.mode}
                  type="button"
                  onClick={() => setMode(option.mode)}
                  aria-pressed={active}
                  className={cn(
                    "flex items-center gap-2 rounded-xl border px-4 py-3 text-sm font-medium transition-colors",
                    active
                      ? "border-[var(--app-accent)] bg-[var(--app-accent-soft)] text-[var(--app-accent)]"
                      : "border-kumo-hairline text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
                  )}
                >
                  <Icon
                    className="size-5"
                    weight={active ? "fill" : "regular"}
                  />
                  {option.label}
                </button>
              );
            })}
          </div>
        </div>

        <div>
          <div className="mb-2 text-sm font-medium">主题色</div>
          <div className="flex flex-wrap items-center gap-3">
            <AccentSwatch
              label="默认"
              color={DEFAULT_ACCENT}
              active={accent === null}
              onClick={() => {
                setShowCustom(false);
                setAccent(null);
              }}
            />
            {ACCENT_PRESETS.map((preset) => (
              <AccentSwatch
                key={preset.id}
                label={preset.label}
                color={preset.color}
                active={accent === preset.color}
                onClick={() => {
                  setShowCustom(false);
                  setAccent(preset.color);
                }}
              />
            ))}
            <button
              type="button"
              title="自定义"
              aria-label="自定义主题色"
              aria-pressed={isCustom}
              onClick={handleSelectCustom}
              className={cn(
                "flex size-9 items-center justify-center rounded-full border border-kumo-hairline text-white transition-transform hover:scale-105",
                isCustom &&
                  "ring-2 ring-[var(--app-accent)] ring-offset-2 ring-offset-[var(--color-kumo-base)]",
              )}
              style={{ backgroundImage: CUSTOM_SWATCH_GRADIENT }}
            >
              <Palette className="size-4 drop-shadow" weight="fill" />
            </button>
          </div>

          {showPicker && (
            <div className="mt-3 max-w-[16rem] space-y-3 rounded-xl border border-kumo-hairline bg-kumo-elevated p-3">
              <HexColorPicker
                color={accent}
                onChange={setAccent}
                style={{ width: "100%" }}
              />
              <HexColorInput
                color={accent}
                onChange={setAccent}
                prefixed
                className="w-full rounded-lg border border-kumo-hairline bg-kumo-base px-2 py-1 font-mono text-xs uppercase text-kumo-default"
              />
            </div>
          )}
        </div>
      </div>
    </Section>
  );
}

function TagDatabaseSection() {
  const { info, updateStatus, update } = useTagTranslation();
  const toast = useToast();
  const updating = updateStatus === "loading";

  const handleUpdate = async () => {
    if (updating) return;
    try {
      const result = await update();
      toast.show({
        title: result.changed ? "标签翻译数据库已更新" : "翻译数据库已是最新",
        description: info?.sha ? `版本 ${info.sha.slice(0, 7)}` : undefined,
        variant: result.changed ? "success" : "info",
      });
    } catch (error) {
      toast.error("更新标签翻译数据库失败", error);
    }
  };

  return (
    <Section
      title="标签翻译数据库"
      action={
        <Button variant="secondary" size="sm" onClick={handleUpdate} disabled={updating}>
          {updating ? (
            <CircleNotch className="mr-1 size-4 animate-spin" />
          ) : (
            <ArrowsClockwise className="mr-1 size-4" weight="bold" />
          )}
          检查更新
        </Button>
      }
    >
      <div className="card-surface p-4 text-sm">
        {info?.sha ? (
          <dl className="space-y-1.5">
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">版本</dt>
              <dd className="tnum">{info.version ?? "—"}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">提交</dt>
              <dd className="font-mono text-xs">{info.sha.slice(0, 7)}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">加载时间</dt>
              <dd>{formatDateTime(info.loadedAt)}</dd>
            </div>
          </dl>
        ) : (
          <p className="text-kumo-subtle">标签翻译数据库未加载</p>
        )}
      </div>
    </Section>
  );
}

function DevToolsSection() {
  const toast = useToast();
  const queryClient = useQueryClient();

  const stateQuery = useQuery({
    queryKey: ["dev-upstream-down"],
    queryFn: fetchUpstreamDown,
    retry: false,
    staleTime: SETTINGS_STALE_TIME,
  });

  const mutation = useMutation({
    mutationFn: (down: boolean) => setUpstreamDown(down),
    onSuccess: (data) => {
      queryClient.setQueryData(["dev-upstream-down"], data);
      toast.show({
        title: data.down ? "已模拟 ExHentai 不可用" : "已恢复正常",
        variant: data.down ? "info" : "success",
      });
    },
    onError: (error) => {
      toast.error("切换失败", error);
    },
  });

  const notEnabled =
    stateQuery.error instanceof ApiRequestError &&
    stateQuery.error.status === 404;

  return (
    <Section title="调试工具">
      <div className="card-surface p-4 text-sm">
        {notEnabled ? (
          <p className="text-kumo-subtle">
            调试接口未开启。设置{" "}
            <code className="rounded bg-kumo-recessed px-1 py-0.5 font-mono text-xs">
              MANGA_READER_DEV_TOOLS=true
            </code>{" "}
            并重启后端后可用。
          </p>
        ) : stateQuery.isLoading ? (
          <div className="flex items-center gap-2 text-kumo-subtle">
            <CircleNotch className="size-4 animate-spin" />
            正在读取…
          </div>
        ) : stateQuery.isError ? (
          <p className="text-kumo-subtle">无法读取调试接口状态。</p>
        ) : (
          <div className="space-y-3">
            <Switch
              label="模拟 ExHentai 不可用"
              checked={stateQuery.data?.down ?? false}
              disabled={mutation.isPending}
              onCheckedChange={(checked) => mutation.mutate(checked)}
            />
            <p className="text-xs text-kumo-subtle">
              仅用于验证离线回退：开启后所有在线端点返回 502，缓存与本地数据端点不受影响。
            </p>
          </div>
        )}
      </div>
    </Section>
  );
}

function SyncSection() {
  const toast = useToast();
  const queryClient = useQueryClient();

  const configQuery = useQuery({
    queryKey: ["sync-config"],
    queryFn: fetchSyncConfig,
    retry: false,
    staleTime: SETTINGS_STALE_TIME,
  });
  const statusQuery = useQuery({
    queryKey: ["sync-status"],
    queryFn: fetchSyncStatus,
    retry: false,
    staleTime: 0,
    refetchInterval: 5000,
  });

  const [serverURL, setServerURL] = useState("");
  const [token, setToken] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [tokenStored, setTokenStored] = useState(false);
  // Adopt loaded config during render (React's "adjust state when a prop
  // changes" pattern); structural sharing keeps the reference stable so
  // typing is not clobbered by unrelated refetches.
  const [appliedConfig, setAppliedConfig] = useState<SyncConfig | null>(null);
  const config = configQuery.data;
  if (config && config !== appliedConfig) {
    setAppliedConfig(config);
    setServerURL(config.server_url);
    setEnabled(config.enabled);
    setTokenStored(config.token === SYNC_KEY_MASK);
  }

  const saveMutation = useMutation({
    mutationFn: (update: SyncConfigUpdate) => saveSyncConfig(update),
    onSuccess: (cfg) => {
      queryClient.setQueryData(["sync-config"], cfg);
      setTokenStored(cfg.token === SYNC_KEY_MASK);
      setToken("");
      void statusQuery.refetch();
      toast.success("同步配置已保存");
    },
    onError: (error) => {
      toast.error("保存同步配置失败", error);
    },
  });

  const handleSave = () => {
    const update: SyncConfigUpdate = { enabled, server_url: serverURL.trim() };
    const trimmedToken = token.trim();
    if (trimmedToken !== "") update.token = trimmedToken;
    saveMutation.mutate(update);
  };

  const handleClearToken = () => {
    setToken("");
    saveMutation.mutate({ token: "" });
  };

  const [resyncOpen, setResyncOpen] = useState(false);
  const resyncMutation = useMutation({
    mutationFn: startFullSync,
    onSuccess: () => {
      setResyncOpen(false);
      void statusQuery.refetch();
      // A resync can change any synced row, so drop every cached list.
      for (const queryKey of [
        ["reading-progress"],
        ["bookshelf"],
        ["recently-read"],
        ["gallery-cache"],
      ]) {
        void queryClient.invalidateQueries({ queryKey });
      }
      toast.success("已开始全量同步");
    },
    onError: (error) => {
      toast.error("全量同步失败", error);
    },
  });

  const status = statusQuery.data;

  return (
    <Section title="数据同步">
      <div className="space-y-4">
        <div className="card-surface space-y-4 p-4 text-sm">
          {configQuery.isLoading ? (
            <div className="flex items-center gap-2 text-kumo-subtle">
              <CircleNotch className="size-4 animate-spin" />
              正在读取…
            </div>
          ) : configQuery.isError ? (
            <p className="text-kumo-subtle">无法读取同步配置。</p>
          ) : (
            <>
              <Switch
                label="启用数据同步"
                checked={enabled}
                disabled={saveMutation.isPending}
                onCheckedChange={setEnabled}
              />
              <div>
                <div className="mb-2 text-sm font-medium">服务端地址</div>
                <Input
                  type="url"
                  placeholder="https://your-server.example.com"
                  aria-label="同步服务端地址"
                  value={serverURL}
                  onChange={(e) => setServerURL(e.target.value)}
                  disabled={saveMutation.isPending}
                />
              </div>
              <div>
                <div className="mb-2 text-sm font-medium">
                  节点 API Key
                  {tokenStored && (
                    <span className="ml-2 text-xs font-normal text-kumo-subtle">
                      已保存
                    </span>
                  )}
                </div>
                <div className="flex gap-2">
                  <Input
                    type="password"
                    autoComplete="off"
                    placeholder={
                      tokenStored ? "留空保持现有令牌" : "在下方「API Key」创建的密钥"
                    }
                    aria-label="节点 API Key"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    disabled={saveMutation.isPending}
                  />
                  {tokenStored && (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={handleClearToken}
                      disabled={saveMutation.isPending}
                    >
                      清除
                    </Button>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button onClick={handleSave} disabled={saveMutation.isPending}>
                  {saveMutation.isPending && (
                    <CircleNotch className="mr-1 size-4 animate-spin" />
                  )}
                  保存
                </Button>
                <p className="text-xs text-kumo-subtle">
                  双向同步阅读进度与书架；密钥在下方「API Key」创建。
                </p>
              </div>
            </>
          )}
        </div>

        <div className="card-surface p-4 text-sm">
          <dl className="space-y-1.5">
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">连接状态</dt>
              <dd className="tnum">
                {!status?.configured
                  ? "未配置"
                  : status.sse_connected
                    ? "实时连接"
                    : "未连接"}
              </dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">待推送变更</dt>
              <dd className="tnum">{status?.pending ?? "—"}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-kumo-subtle">最近同步</dt>
              <dd className="tnum">
                {status?.last_sync_at ? formatDateTime(status.last_sync_at) : "—"}
              </dd>
            </div>
            {status?.last_error && (
              <div className="flex justify-between gap-4">
                <dt className="text-kumo-subtle">最近错误</dt>
                <dd className="text-right text-xs text-red-500">
                  {status.last_error}
                </dd>
              </div>
            )}
          </dl>
          <div className="mt-3 flex items-start justify-between gap-3 border-t border-kumo-hairline pt-3">
            <div>
              <div className="font-medium">全量同步</div>
              <p className="mt-0.5 text-xs text-kumo-subtle">
                重新交换两端全量数据，修复不一致。
              </p>
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setResyncOpen(true)}
              disabled={!status?.configured || resyncMutation.isPending}
            >
              {resyncMutation.isPending ? (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              ) : (
                <ArrowsClockwise className="mr-1 size-4" weight="bold" />
              )}
              全量同步
            </Button>
          </div>
        </div>
      </div>

      <Dialog.Root open={resyncOpen} onOpenChange={setResyncOpen}>
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            开始全量同步？
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            将重新交换两端完整数据，按更新时间合并。
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              onClick={() => resyncMutation.mutate()}
              disabled={resyncMutation.isPending}
            >
              {resyncMutation.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              开始同步
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </Section>
  );
}

type SectionShow = "always" | "global" | "admin";

interface SectionDef {
  id: string;
  label: string;
  group: "general" | "tags" | "sync" | "debug" | "admin";
  show: SectionShow;
  element: ReactNode;
}

const SECTION_DEFS: SectionDef[] = [
  {
    id: "appearance",
    label: "外观",
    group: "general",
    show: "always",
    element: <AppearanceSection />,
  },
  {
    id: "cookie",
    label: "账号",
    group: "general",
    show: "always",
    element: <CookieSection />,
  },
  {
    id: "tag-rules",
    label: "标签规则",
    group: "tags",
    show: "always",
    element: <TagRulesSection />,
  },
  {
    id: "tag-database",
    label: "标签翻译数据库",
    group: "tags",
    show: "always",
    element: <TagDatabaseSection />,
  },
  {
    id: "sync",
    label: "数据同步",
    group: "sync",
    show: "global",
    element: <SyncSection />,
  },
  {
    id: "users",
    label: "用户管理",
    group: "admin",
    show: "admin",
    element: <UserManagementSection />,
  },
  {
    id: "api-keys",
    label: "API Key",
    group: "admin",
    show: "admin",
    element: <ApiManagementSection />,
  },
  {
    id: "dev-tools",
    label: "调试工具",
    group: "debug",
    show: "global",
    element: <DevToolsSection />,
  },
];

const GROUP_ORDER = ["general", "tags", "sync", "admin", "debug"] as const;

const GROUP_LABELS: Record<(typeof GROUP_ORDER)[number], string> = {
  general: "通用",
  tags: "标签",
  sync: "同步",
  debug: "调试",
  admin: "管理",
};

export function SettingsPage() {
  useDocumentTitle("设置");
  const { isAdmin } = useAuth();
  const canManageGlobal = isAdmin;

  const { sections, groups } = useMemo(() => {
    const visible = SECTION_DEFS.filter(
      (d) =>
        d.show === "always" ||
        (d.show === "global" && canManageGlobal) ||
        (d.show === "admin" && isAdmin),
    );
    const groups = GROUP_ORDER.map((groupId) => ({
      id: groupId,
      label: GROUP_LABELS[groupId],
      items: visible
        .filter((d) => d.group === groupId)
        .map((d) => ({ id: d.id, label: d.label })),
    })).filter((group) => group.items.length > 0);
    return { sections: visible, groups };
  }, [canManageGlobal, isAdmin]);

  const sectionIds = useMemo(
    () => sections.map((d) => d.id),
    [sections],
  );

  const [activeId, setActiveId] = useState<string>(sectionIds[0] ?? "");
  const [mobileSectionId, setMobileSectionId] = useState<string | null>(null);
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  // Desktop renders only the selected section (switch pages, not scroll).
  const activeSection =
    sections.find((d) => d.id === activeId) ?? sections[0];

  const navigateTo = (id: string) => {
    setActiveId(id);
  };

  const selectedSection = mobileSectionId
    ? sections.find((d) => d.id === mobileSectionId)
    : undefined;

  const openMobileSection = (id: string) => {
    setMobileSectionId(id);
    window.scrollTo(0, 0);
  };

  const closeMobileSection = () => {
    setMobileSectionId(null);
    window.scrollTo(0, 0);
  };

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader title="设置" />

      {isDesktop ? (
        <div className="grid grid-cols-[15rem_minmax(0,1fr)] gap-10">
          <aside>
            <div className="sticky top-20">
              <SettingsSidebar
                groups={groups}
                activeId={activeSection?.id ?? ""}
                onNavigate={navigateTo}
              />
            </div>
          </aside>

          <div className="min-w-0">
            {activeSection && (
              <div key={activeSection.id} className="settings-section-in">
                {activeSection.element}
              </div>
            )}
          </div>
        </div>
      ) : selectedSection ? (
        <div>
          <button
            type="button"
            onClick={closeMobileSection}
            className="mb-3 inline-flex items-center gap-1 text-sm font-medium text-kumo-subtle transition-colors hover:text-kumo-default"
          >
            <CaretLeft className="size-4" weight="bold" />
            返回设置
          </button>
          <div key={selectedSection.id} className="settings-section-slide">
            {selectedSection.element}
          </div>
        </div>
      ) : (
        <nav aria-label="设置导航">
          {groups.map((group) => (
            <div key={group.id} className="mb-5">
              <h2 className="mb-1.5 px-1 text-xs font-medium text-kumo-subtle">
                {group.label}
              </h2>
              <ul className="space-y-0.5">
                {group.items.map((item) => (
                  <li key={item.id}>
                    <button
                      type="button"
                      onClick={() => openMobileSection(item.id)}
                      className="flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-sm text-kumo-default transition-colors hover:bg-kumo-tint"
                    >
                      {item.label}
                      <CaretRight className="size-4 text-kumo-subtle" />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
      )}
    </div>
  );
}
