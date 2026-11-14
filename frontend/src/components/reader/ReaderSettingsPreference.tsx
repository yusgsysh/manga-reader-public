import { Switch } from "@cloudflare/kumo";
import { useReaderPreferences } from "../../hooks/useReaderPreferences";

/**
 * The one reader option that belongs to the app rather than to comimi.
 * Rendered through a portal into comimi's settings sheet so it sits with the
 * library's own settings.
 */
export function ReaderSettingsPreference() {
  const { autoFullscreenOnStart, setAutoFullscreenOnStart } =
    useReaderPreferences();

  return (
    <div className="mt-5 border-t border-kumo-hairline pt-4">
      <Switch
        label="开始阅读时自动全屏"
        checked={autoFullscreenOnStart}
        onCheckedChange={setAutoFullscreenOnStart}
      />
      <p className="mt-1.5 text-[11px] leading-relaxed text-kumo-subtle">
        从画廊点「开始阅读」时直接进入全屏；浏览器不允许时至少占满窗口。
      </p>
    </div>
  );
}
