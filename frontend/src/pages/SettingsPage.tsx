import {
  ArrowsClockwise,
  CircleNotch,
  Gear,
  Monitor,
  Moon,
  Sun,
} from "@phosphor-icons/react";
import {
  Button,
  Switch,
  useKumoToastManager,
  cn,
} from "@cloudflare/kumo";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchUpstreamDown, setUpstreamDown } from "../api/dev";
import { ApiRequestError } from "../api/client";
import { useTheme } from "../hooks/useTheme";
import { useTagTranslation } from "../hooks/useTagTranslation";
import { SETTINGS_STALE_TIME } from "../lib/cacheConfig";
import { formatDateTime } from "../lib/time";
import type { ThemeMode } from "../lib/theme";
import { PageHeader, Section } from "../components/ui";

const THEME_OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "浅色", icon: Sun },
  { mode: "dark", label: "深色", icon: Moon },
  { mode: "system", label: "跟随系统", icon: Monitor },
];

function AppearanceSection() {
  const { mode, setMode } = useTheme();

  return (
    <Section title="外观">
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
              <Icon className="size-5" weight={active ? "fill" : "regular"} />
              {option.label}
            </button>
          );
        })}
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

function DevToolsSection() {
  const toast = useKumoToastManager();
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

export function SettingsPage() {
  return (
    <div className="max-w-3xl">
      <PageHeader
        title="设置"
        icon={<Gear className="size-5" weight="fill" />}
      />
      <div className="space-y-8">
        <AppearanceSection />
        <TagDatabaseSection />
        <DevToolsSection />
      </div>
    </div>
  );
}
