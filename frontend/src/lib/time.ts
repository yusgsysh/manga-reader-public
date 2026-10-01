// Absolute timestamps are rendered in the viewer's local timezone (browser),
// unless noted otherwise.

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

const dateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

function toDate(value?: string | number | Date | null): Date | null {
  if (value === undefined || value === null || value === "") return null;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDate(value?: string | number | Date | null): string {
  const date = toDate(value);
  if (!date) return "";
  return dateFormatter.format(date).replace(/\//g, "-");
}

export function formatDateTime(value?: string | number | Date | null): string {
  const date = toDate(value);
  if (!date) return "";
  return dateTimeFormatter.format(date).replace(/\//g, "-");
}

/**
 * ExHentai "posted" strings come either as a date only ("2024-01-01") or as a
 * UTC datetime ("2024-01-01 12:34"). Date-only values are passed through, while
 * datetimes are converted from UTC to the browser's local timezone.
 */
export function formatPosted(posted?: string | null): string {
  if (!posted) return "";
  const trimmed = posted.trim();

  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) return trimmed;

  const match = trimmed.match(
    /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/,
  );
  if (match) {
    const utc = Date.UTC(
      Number(match[1]),
      Number(match[2]) - 1,
      Number(match[3]),
      Number(match[4]),
      Number(match[5]),
    );
    return formatDateTime(new Date(utc));
  }

  return trimmed;
}

export function formatRelativeTime(iso?: string | null): string {
  const date = toDate(iso);
  if (!date) return "";
  const timestamp = date.getTime();

  const diffMs = Date.now() - timestamp;
  const diffMin = Math.floor(diffMs / 60_000);

  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin} 分钟前`;

  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;

  const diffDay = Math.floor(diffHour / 24);
  if (diffDay === 1) return "昨天";
  if (diffDay < 30) return `${diffDay} 天前`;

  return formatDate(date);
}
