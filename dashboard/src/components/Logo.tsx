// UptimeX logo mark: an ink rounded-square badge with a mint ECG pulse —
// the product in one glyph (a heartbeat of monitored uptime).
export function Logo({
  size = 28,
  wordmark = false,
}: {
  size?: number;
  wordmark?: boolean;
}) {
  return (
    <span className="logo">
      <svg width={size} height={size} viewBox="0 0 48 48" aria-hidden="true">
        <rect x="1.5" y="1.5" width="45" height="45" rx="13" fill="var(--ink)" />
        <path
          d="M8 26 H14 L17.5 14 L22 34 L26 22 L29 26 H40"
          fill="none"
          stroke="var(--mint)"
          strokeWidth={3.2}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      {wordmark && <span className="logo-word">UptimeX</span>}
    </span>
  );
}
