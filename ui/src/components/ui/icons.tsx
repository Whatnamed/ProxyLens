import React from 'react';

interface IconProps {
  size?: number;
}

const base = (size: number) => ({
  width: size,
  height: size,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.7,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
});

export const IconOverview: React.FC<IconProps> = ({ size = 15 }) => (
  <svg {...base(size)}>
    <rect x="3.5" y="3.5" width="7" height="9" rx="1" />
    <rect x="13.5" y="3.5" width="7" height="5" rx="1" />
    <rect x="13.5" y="11.5" width="7" height="9" rx="1" />
    <rect x="3.5" y="15.5" width="7" height="5" rx="1" />
  </svg>
);

export const IconHistory: React.FC<IconProps> = ({ size = 15 }) => (
  <svg {...base(size)}>
    <path d="M4 6h16" />
    <path d="M4 12h16" />
    <path d="M4 18h10" />
  </svg>
);

export const IconCoverage: React.FC<IconProps> = ({ size = 15 }) => (
  <svg {...base(size)}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 7.5V12l3 2.5" />
  </svg>
);

export const IconRefresh: React.FC<IconProps> = ({ size = 14 }) => (
  <svg {...base(size)}>
    <path d="M20 12a8 8 0 1 1-2.34-5.66" />
    <path d="M20 4v4h-4" />
  </svg>
);

export const IconClose: React.FC<IconProps> = ({ size = 14 }) => (
  <svg {...base(size)}>
    <path d="M6 6l12 12" />
    <path d="M18 6L6 18" />
  </svg>
);

export const IconCopy: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <rect x="9" y="9" width="11" height="11" rx="1.5" />
    <path d="M5 15V5a1 1 0 0 1 1-1h9" />
  </svg>
);

export const IconSnapshot: React.FC<IconProps> = ({ size = 12 }) => (
  <svg {...base(size)}>
    <rect x="5" y="10" width="14" height="10" rx="1.5" />
    <path d="M8 10V7a4 4 0 0 1 8 0v3" />
  </svg>
);

export const IconSun: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5 5l1.4 1.4M17.6 17.6L19 19M19 5l-1.4 1.4M6.4 17.6L5 19" />
  </svg>
);

export const IconMoon: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <path d="M20 13.5A8 8 0 1 1 10.5 4a6.5 6.5 0 0 0 9.5 9.5z" />
  </svg>
);

export const IconLens: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <circle cx="10.5" cy="10.5" r="6" />
    <path d="M15 15l5.5 5.5" />
  </svg>
);

export const IconArrowRight: React.FC<IconProps> = ({ size = 12 }) => (
  <svg {...base(size)}>
    <path d="M4 12h15" />
    <path d="M13 6l6 6-6 6" />
  </svg>
);

export const IconChevronDown: React.FC<IconProps> = ({ size = 12 }) => (
  <svg {...base(size)}>
    <path d="M6 9l6 6 6-6" />
  </svg>
);

export const IconChevronLeft: React.FC<IconProps> = ({ size = 14 }) => (
  <svg {...base(size)}>
    <path d="M14.5 5.5L8 12l6.5 6.5" />
  </svg>
);

export const IconChevronRight: React.FC<IconProps> = ({ size = 14 }) => (
  <svg {...base(size)}>
    <path d="M9.5 5.5L16 12l-6.5 6.5" />
  </svg>
);

export const IconCheck: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <path d="M5 12.5l4 4L19 7" />
  </svg>
);

export const IconCalendar: React.FC<IconProps> = ({ size = 13 }) => (
  <svg {...base(size)}>
    <rect x="4" y="5.5" width="16" height="14" rx="1.5" />
    <path d="M8 3.5v4M16 3.5v4M4 10h16" />
  </svg>
);
