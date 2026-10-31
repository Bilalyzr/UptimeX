// Minimal stroke-icon set (24-grid, 1.8 stroke, round caps) drawn in
// currentColor so they inherit menu and heading colors — used by the
// branched menu and the profile page section headers.

type IconProps = { size?: number };

function svgProps(size: number) {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
  };
}

export function IconGrid({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <rect x="3.5" y="3.5" width="7" height="7" rx="1.5" />
      <rect x="13.5" y="3.5" width="7" height="7" rx="1.5" />
      <rect x="3.5" y="13.5" width="7" height="7" rx="1.5" />
      <rect x="13.5" y="13.5" width="7" height="7" rx="1.5" />
    </svg>
  );
}

export function IconGlobe({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17" />
      <path d="M12 3.5c2.6 2.3 4 5.2 4 8.5s-1.4 6.2-4 8.5c-2.6-2.3-4-5.2-4-8.5s1.4-6.2 4-8.5z" />
    </svg>
  );
}

export function IconAlert({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <path d="M12 4 21 19.5H3z" />
      <path d="M12 10v4.5" />
      <circle cx="12" cy="17.2" r="0.4" />
    </svg>
  );
}

export function IconCard({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <rect x="3" y="5.5" width="18" height="13" rx="2.5" />
      <path d="M3 10h18" />
      <path d="M6.5 14.5h4" />
    </svg>
  );
}

export function IconUser({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <circle cx="12" cy="8" r="4" />
      <path d="M4.5 20.5c1.3-3.4 4-5 7.5-5s6.2 1.6 7.5 5" />
    </svg>
  );
}

export function IconLock({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <rect x="5" y="10.5" width="14" height="9.5" rx="2.5" />
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
      <path d="M12 14.5v2" />
    </svg>
  );
}

export function IconMonitor({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <rect x="3" y="4.5" width="18" height="12.5" rx="2" />
      <path d="M9 21h6M12 17v4" />
    </svg>
  );
}

export function IconBuilding({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <rect x="5" y="3.5" width="14" height="17" rx="1.5" />
      <path d="M9 7.5h2M13 7.5h2M9 11.5h2M13 11.5h2M9 15.5h2M13 15.5h2" />
      <path d="M3 20.5h18" />
    </svg>
  );
}

export function IconBell({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <path d="M6 10a6 6 0 0 1 12 0c0 4 1.8 5.8 1.8 5.8H4.2S6 14 6 10z" />
      <path d="M10.3 19a2 2 0 0 0 3.4 0" />
    </svg>
  );
}

export function IconKey({ size = 16 }: IconProps) {
  return (
    <svg {...svgProps(size)}>
      <circle cx="8" cy="15" r="4" />
      <path d="M11 12l8-8" />
      <path d="M16 7l2.5 2.5" />
      <path d="M18.5 4.5L21 7" />
    </svg>
  );
}
