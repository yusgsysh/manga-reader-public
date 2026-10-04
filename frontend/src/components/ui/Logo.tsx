interface LogoProps {
  className?: string;
  showWordmark?: boolean;
}

export function Logo({ className, showWordmark = true }: LogoProps) {
  return (
    <span className={`inline-flex items-center gap-2 ${className ?? ""}`}>
      <svg
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden
        className="size-6 shrink-0"
      >
        <rect
          x="2.5"
          y="4"
          width="19"
          height="16"
          rx="3.5"
          fill="var(--color-kumo-base)"
        />
        <path
          d="M12 7.54c-1.4-1.31-3.24-1.92-5.25-1.92H4.56v10.85h2.19c2.01 0 3.85.61 5.25 1.92z"
          fill="none"
          stroke="var(--app-accent)"
          strokeWidth="1.6"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M12 7.54c1.4-1.31 3.24-1.92 5.25-1.92h2.19v10.85h-2.19c-2.01 0-3.85.61-5.25 1.92z"
          fill="none"
          stroke="var(--app-accent)"
          strokeWidth="1.6"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      {showWordmark && (
        <span className="text-[15px] font-semibold tracking-tight">
          Manga Reader
        </span>
      )}
    </span>
  );
}
