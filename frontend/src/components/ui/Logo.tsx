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
          fill="var(--app-accent)"
        />
        <path
          d="M12 7.75v8.5"
          stroke="white"
          strokeWidth="1.6"
          strokeLinecap="round"
        />
        <path
          d="M6.75 9.5H9M6.75 12.5H9M15 9.5h2.25M15 12.5h2.25"
          stroke="white"
          strokeWidth="1.4"
          strokeLinecap="round"
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
