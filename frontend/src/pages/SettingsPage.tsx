import { useState } from "react";
import {
  ArrowsClockwise,
  CircleNotch,
  Gear,
  Monitor,
  Moon,
  Palette,
  Sun,
} from "@phosphor-icons/react";
import {
  Button,
  Input,
  Select,
  SensitiveInput,
  Switch,
  useKumoToastManager,
  cn,
} from "@cloudflare/kumo";
import { HexColorPicker, HexColorInput } from "react-colorful";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchUpstreamDown, setUpstreamDown } from "../api/dev";
import {
  fetchSettings,
  saveSettings,
  type SettingsSnapshot,
  type SettingsUpdate,
} from "../api/settings";
import { ApiRequestError } from "../api/client";
import { useTheme } from "../hooks/useTheme";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { SETTINGS_STALE_TIME } from "../lib/cacheConfig";
import { ACCENT_PRESETS, DEFAULT_ACCENT, isPresetAccent } from "../lib/accent";
import { formatDateTime } from "../lib/time";
import type { ThemeMode } from "../lib/theme";
import { PageHeader, Section } from "../components/ui";
import { useDocumentTitle } from "../hooks/useDocumentTitle";

const SETTINGS_QUERY_KEY = ["settings"] as const;

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

const LOG_LEVELS = [
  { value: "debug", label: "debug · 调试" },
  { value: "info", label: "info · 信息" },
  { value: "warn", label: "warn · 警告" },
  { value: "error", label: "error · 错误" },
];

const STORAGE_DRIVERS = [
  { value: "auto", label: "自动" },
  { value: "local", label: "本地目录" },
  { value: "s3", label: "S3 / MinIO 对象存储" },
];

const PATH_STYLES = [
  { value: "auto", label: "auto · 自动（推荐）" },
  { value: "path", label: "path · 路径风格" },
  { value: "dns", label: "dns · 虚拟主机" },
];

const CUSTOM_ACCENT_SEED = "#6366f1";

const CUSTOM_SWATCH_GRADIENT =
  "conic-gradient(from 0deg, #ef4444, #eab308, #22c55e, #06b6d4, #3b82f6, #a855f7, #ef4444)";

/**
 * Applies one section to the server and returns the resulting snapshot, or null
 * when the save was rejected. The caller resets its form from that snapshot so
 * the masked secrets it gets back match what the backend now stores.
 */
type SaveSettings = (
  label: string,
  update: SettingsUpdate,
) => Promise<SettingsSnapshot | null>;

interface ServerSectionProps {
  value: SettingsSnapshot;
  onSave: SaveSettings;
}

function SaveButton({
  onClick,
  saving,
}: {
  onClick: () => void;
  saving: boolean;
}) {
  return (
    <Button
      variant="primary"
      size="sm"
      onClick={onClick}
      disabled={saving}
    >
      {saving ? (
        <CircleNotch className="mr-1 size-4 animate-spin" />
      ) : (
        <ArrowsClockwise className="mr-1 size-4" weight="bold" />
      )}
      保存
    </Button>
  );
}

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

function AccountSection({ value, onSave }: ServerSectionProps) {
  const [form, setForm] = useState(() => ({ ...value.cookie }));
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    try {
      // Reloading from the response re-masks the secrets and reflects anything
      // the backend normalised on the way in.
      const saved = await onSave("ExHentai 账号", {
        cookie: {
          memberId: form.memberId,
          passHash: form.passHash,
          igneous: form.igneous,
          sk: form.sk,
        },
      });
      if (saved) setForm({ ...saved.cookie });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Section
      title="ExHentai 账号"
      description="Cookie 变更立即生效，无需重启后端。"
      action={<SaveButton onClick={handleSave} saving={saving} />}
    >
      <div className="card-surface space-y-4 p-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label="ipb_member_id"
            value={form.memberId}
            onChange={(e) => setForm({ ...form, memberId: e.target.value })}
            autoComplete="off"
            spellCheck={false}
          />
          <SensitiveInput
            label="ipb_pass_hash"
            value={form.passHash}
            onValueChange={(v) => setForm({ ...form, passHash: v })}
            autoComplete="off"
            spellCheck={false}
          />
          <SensitiveInput
            label="igneous"
            description="可选，部分画廊需要。"
            value={form.igneous}
            onValueChange={(v) => setForm({ ...form, igneous: v })}
            autoComplete="off"
            spellCheck={false}
          />
          <SensitiveInput
            label="sk"
            description="可选。"
            value={form.sk}
            onValueChange={(v) => setForm({ ...form, sk: v })}
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        {/* Derived from the form so it reflects what the user is about to save. */}
        <p
          className={cn(
            "text-xs",
            form.memberId && form.passHash ? "text-kumo-subtle" : "text-red-500",
          )}
        >
          {form.memberId && form.passHash
            ? "Cookie 已配置，可以访问 ExHentai。"
            : "尚未配置：ipb_member_id 与 ipb_pass_hash 都必须填写。"}
        </p>
      </div>
    </Section>
  );
}

function StorageSection({ value, onSave }: ServerSectionProps) {
  const [form, setForm] = useState(() => ({
    driver: value.storage.driver,
    dir: value.storage.dir,
    ...value.storage.s3,
  }));
  const [saving, setSaving] = useState(false);

  const showS3 = form.driver !== "local";

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    try {
      const saved = await onSave("图片缓存", {
        storage: {
          driver: form.driver,
          dir: form.dir,
          s3: {
            endpoint: form.endpoint,
            region: form.region,
            bucket: form.bucket,
            accessKey: form.accessKey,
            secretKey: form.secretKey,
            useSsl: form.useSsl,
            pathStyle: form.pathStyle,
          },
        },
      });
      if (saved) {
        setForm({
          driver: saved.storage.driver,
          dir: saved.storage.dir,
          ...saved.storage.s3,
        });
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Section
      title="图片缓存存储"
      description="切换存储后立即生效，正在读取的请求不受影响。"
      action={<SaveButton onClick={handleSave} saving={saving} />}
    >
      <div className="card-surface space-y-4 p-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Select
            label="存储驱动"
            value={form.driver}
            onValueChange={(v) => v != null && setForm({ ...form, driver: v })}
          >
            {STORAGE_DRIVERS.map((option) => (
              <Select.Option key={option.value} value={option.value}>
                {option.label}
              </Select.Option>
            ))}
          </Select>
          <Input
            label="本地缓存目录"
            description={
              form.driver === "s3" ? "s3 驱动下不使用该目录。" : "相对后端工作目录。"
            }
            value={form.dir}
            onChange={(e) => setForm({ ...form, dir: e.target.value })}
            spellCheck={false}
          />
        </div>

        <p className="text-xs text-kumo-subtle">
          当前实际使用：
          <span className="font-medium text-kumo-default">
            {value.storage.resolvedDriver === "s3"
              ? "S3 / MinIO 对象存储"
              : "本地目录"}
          </span>
        </p>

        {showS3 && (
          <div className="grid gap-4 border-t border-kumo-hairline pt-4 sm:grid-cols-2">
            <Input
              label="endpoint"
              description="不含协议，例如 minio:9000 或 s3.amazonaws.com。"
              value={form.endpoint}
              onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
              spellCheck={false}
            />
            <Input
              label="region"
              description="留空使用 us-east-1。"
              value={form.region}
              onChange={(e) => setForm({ ...form, region: e.target.value })}
              spellCheck={false}
            />
            <Input
              label="bucket"
              value={form.bucket}
              onChange={(e) => setForm({ ...form, bucket: e.target.value })}
              spellCheck={false}
            />
            <Input
              label="accessKey"
              value={form.accessKey}
              onChange={(e) => setForm({ ...form, accessKey: e.target.value })}
              autoComplete="off"
              spellCheck={false}
            />
            <SensitiveInput
              label="secretKey"
              value={form.secretKey}
              onValueChange={(v) => setForm({ ...form, secretKey: v })}
              autoComplete="off"
              spellCheck={false}
            />
            <Select
              label="pathStyle"
              value={form.pathStyle}
              onValueChange={(v) => v != null && setForm({ ...form, pathStyle: v })}
            >
              {PATH_STYLES.map((option) => (
                <Select.Option key={option.value} value={option.value}>
                  {option.label}
                </Select.Option>
              ))}
            </Select>
            <div className="sm:col-span-2">
              <Switch
                label="使用 HTTPS（useSsl）"
                checked={form.useSsl}
                onCheckedChange={(checked) =>
                  setForm({ ...form, useSsl: checked })
                }
              />
            </div>
          </div>
        )}
      </div>
    </Section>
  );
}

function GeneralSection({ value, onSave }: ServerSectionProps) {
  const [saving, setSaving] = useState<null | "logLevel" | "devTools">(null);

  const update = async (
    kind: "logLevel" | "devTools",
    payload: SettingsUpdate,
    label: string,
  ) => {
    if (saving) return;
    setSaving(kind);
    try {
      await onSave(label, payload);
    } finally {
      setSaving(null);
    }
  };

  return (
    <Section
      title="通用"
      description="日志级别与开发工具开关，改动立即生效。"
    >
      <div className="card-surface space-y-4 p-4">
        <Select
          label="日志级别（LOG_LEVEL）"
          value={value.logLevel}
          disabled={saving !== null}
          onValueChange={(v) => {
            if (v == null) return;
            void update("logLevel", { logLevel: v }, "日志级别");
          }}
        >
          {LOG_LEVELS.map((option) => (
            <Select.Option key={option.value} value={option.value}>
              {option.label}
            </Select.Option>
          ))}
        </Select>
        <div className="space-y-1.5">
          <Switch
            label="开发工具（/api/dev/*）"
            checked={value.devTools}
            disabled={saving !== null}
            onCheckedChange={(checked) =>
              update("devTools", { devTools: checked }, "开发工具")
            }
          />
          <p className="text-xs text-kumo-subtle">
            开启后可在下方模拟 ExHentai 不可用，用于验证离线回退。
          </p>
        </div>
      </div>
    </Section>
  );
}

function TagDatabaseSection() {
  const { info, updateStatus, update } = useTagTranslation();
  const toast = useKumoToastManager();
  const updating = updateStatus === "loading";

  const handleUpdate = async () => {
    if (updating) return;
    try {
      const result = await update();
      toast.add({
        title: result.changed ? "标签翻译数据库已更新" : "翻译数据库已是最新",
        description: info?.sha ? `版本 ${info.sha.slice(0, 7)}` : undefined,
        variant: result.changed ? "success" : "info",
      });
    } catch (error) {
      toast.add({
        title: "更新标签翻译数据库失败",
        description: error instanceof Error ? error.message : undefined,
        variant: "error",
      });
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

function DevToolsSection({ devToolsEnabled }: { devToolsEnabled: boolean }) {
  const toast = useKumoToastManager();
  const queryClient = useQueryClient();

  const stateQuery = useQuery({
    queryKey: ["dev-upstream-down"],
    queryFn: fetchUpstreamDown,
    retry: false,
    enabled: devToolsEnabled,
    staleTime: SETTINGS_STALE_TIME,
  });

  const mutation = useMutation({
    mutationFn: (down: boolean) => setUpstreamDown(down),
    onSuccess: (data) => {
      queryClient.setQueryData(["dev-upstream-down"], data);
      toast.add({
        title: data.down ? "已模拟 ExHentai 不可用" : "已恢复正常",
        variant: data.down ? "info" : "success",
      });
    },
    onError: (error) => {
      toast.add({
        title: "切换失败",
        description: error instanceof Error ? error.message : undefined,
        variant: "error",
      });
    },
  });

  const notEnabled =
    !devToolsEnabled ||
    (stateQuery.error instanceof ApiRequestError &&
      stateQuery.error.status === 404);

  return (
    <Section title="调试工具">
      <div className="card-surface p-4 text-sm">
        {notEnabled ? (
          <p className="text-kumo-subtle">
            调试接口未开启。在上方「通用」中打开开发工具开关即可使用，改动立即生效。
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

function SettingsSkeleton() {
  return (
    <div className="space-y-4 text-sm text-kumo-subtle">
      <div className="flex items-center gap-2">
        <CircleNotch className="size-4 animate-spin" />
        正在读取服务器配置…
      </div>
    </div>
  );
}

export function SettingsPage() {
  useDocumentTitle("设置");
  const toast = useKumoToastManager();
  const queryClient = useQueryClient();

  const settingsQuery = useQuery({
    queryKey: SETTINGS_QUERY_KEY,
    queryFn: fetchSettings,
    retry: 1,
    staleTime: SETTINGS_STALE_TIME,
  });

  // TanStack Query owns the snapshot: a successful save writes it back through
  // setQueryData, so every section sees the new values without a refetch.
  const value = settingsQuery.data;

  const handleSave: SaveSettings = async (label, update) => {
    try {
      const saved = await saveSettings(update);
      queryClient.setQueryData(SETTINGS_QUERY_KEY, saved);
      toast.add({ title: `${label}已保存并立即生效`, variant: "success" });
      return saved;
    } catch (error) {
      toast.add({
        title: `${label}保存失败`,
        description: error instanceof Error ? error.message : undefined,
        variant: "error",
      });
      return null;
    }
  };

  return (
    <div className="max-w-3xl">
      <PageHeader
        title="设置"
        icon={<Gear className="size-5" weight="fill" />}
      />
      <div className="space-y-8">
        <AppearanceSection />
        {settingsQuery.isError ? (
          <Section title="服务器配置">
            <div className="card-surface p-4 text-sm text-kumo-subtle">
              无法读取服务器配置：{" "}
              {settingsQuery.error instanceof Error
                ? settingsQuery.error.message
                : "未知错误"}
            </div>
          </Section>
        ) : value === undefined ? (
          <SettingsSkeleton />
        ) : (
          <>
            <AccountSection value={value} onSave={handleSave} />
            <StorageSection value={value} onSave={handleSave} />
            <GeneralSection value={value} onSave={handleSave} />
          </>
        )}
        <TagDatabaseSection />
        <DevToolsSection devToolsEnabled={value?.devTools ?? false} />
      </div>
    </div>
  );
}
