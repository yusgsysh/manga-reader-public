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
 * UTC datetime ("2024-01-01 12:34"). Datetimes are converted from UTC to the
 * browser's local timezone and rendered relative to now:
 *   - within the last hour: "10分钟前"
 *   - earlier today:        "今天10:00"
 *   - earlier this year:    "8/29 10:00"
 *   - any other year:       "2024/1/1 10:00"
 * Date-only values render as "2024/1/1". Unrecognized values pass through.
 */
export function formatPosted(
  posted?: string | null,
  now: Date = new Date(),
): string {
  if (!posted) return "";
  const trimmed = posted.trim();

  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) {
    const [year, month, day] = trimmed.split("-");
    return `${year}/${Number(month)}/${Number(day)}`;
  }

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
    const date = new Date(utc);

    const diffMs = now.getTime() - date.getTime();
    if (diffMs >= 0 && diffMs < 3_600_000) {
      const diffMin = Math.floor(diffMs / 60_000);
      return diffMin < 1 ? "刚刚" : `${diffMin}分钟前`;
    }

    const hh = String(date.getHours()).padStart(2, "0");
    const mm = String(date.getMinutes()).padStart(2, "0");
    if (
      date.getFullYear() === now.getFullYear() &&
      date.getMonth() === now.getMonth() &&
      date.getDate() === now.getDate()
    ) {
      return `今天${hh}:${mm}`;
    }
    if (date.getFullYear() === now.getFullYear()) {
      return `${date.getMonth() + 1}/${date.getDate()} ${hh}:${mm}`;
    }
    return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${hh}:${mm}`;
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
