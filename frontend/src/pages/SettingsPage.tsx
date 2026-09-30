import { Link } from "react-router";
import {
  ArrowsClockwise,
  CircleNotch,
  FolderSimple,
  Gear,
  Monitor,
  Moon,
  Sun,
} from "@phosphor-icons/react";
import { Button, useKumoToastManager, cn } from "@cloudflare/kumo";
import { useTheme } from "../hooks/useTheme";
import { useTagTranslation } from "../hooks/useTagTranslation";
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
    <Section title="外观" description="选择界面主题">
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
      description="用于将日文/英文标签翻译为中文"
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
              <dd>{new Date(info.loadedAt).toLocaleString()}</dd>
            </div>
          </dl>
        ) : (
          <p className="text-kumo-subtle">标签翻译数据库未加载</p>
        )}
      </div>
    </Section>
  );
}

function DataSection() {
  return (
    <Section title="数据" description="管理本地缓存与记录">
      <div className="grid gap-2 sm:grid-cols-2">
        <Link
          to="/recently-read"
          className="card-surface flex items-center gap-3 p-4 text-sm transition-colors hover:bg-kumo-tint"
        >
          <FolderSimple className="size-5 text-kumo-subtle" />
          <span>
            <span className="block font-medium">阅读记录</span>
            <span className="block text-xs text-kumo-subtle">
              查看与清理最近阅读
            </span>
          </span>
        </Link>
        <Link
          to="/downloads"
          className="card-surface flex items-center gap-3 p-4 text-sm transition-colors hover:bg-kumo-tint"
        >
          <FolderSimple className="size-5 text-kumo-subtle" />
          <span>
            <span className="block font-medium">下载记录</span>
            <span className="block text-xs text-kumo-subtle">
              查看与清理下载任务
            </span>
          </span>
        </Link>
      </div>
    </Section>
  );
}

export function SettingsPage() {
  return (
    <div className="max-w-3xl">
      <PageHeader
        title="设置"
        description="外观、翻译数据库与数据管理"
        icon={<Gear className="size-5" weight="fill" />}
      />
      <div className="space-y-8">
        <AppearanceSection />
        <TagDatabaseSection />
        <DataSection />
      </div>
    </div>
  );
}
