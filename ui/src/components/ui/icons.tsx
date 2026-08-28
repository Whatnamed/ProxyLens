import React from 'react';

/**
 * Minimal stroke icon set, drawn inline.
 * ProxyLens is an offline desktop tool, so no icon font or remote asset is
 * used. All icons inherit `currentColor` and default to 14px.
 */

interface IconProps {
  size?: number;
  className?: string;
}

function svgProps(size: number, className?: string) {
  return {
    width: size,
    height: size,
    viewBox: '0 0 16 16',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.4,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    className,
    'aria-hidden': true,
    focusable: false
  };
}

export const IconOverview: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <rect x="2" y="2" width="5" height="5" rx="1" />
    <rect x="9" y="2" width="5" height="5" rx="1" />
    <rect x="2" y="9" width="5" height="5" rx="1" />
    <rect x="9" y="9" width="5" height="5" rx="1" />
  </svg>
);

export const IconHistory: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
    <circle cx="5" cy="4" r="1" fill="currentColor" stroke="none" />
    <circle cx="9.5" cy="8" r="1" fill="currentColor" stroke="none" />
    <circle cx="6" cy="12" r="1" fill="currentColor" stroke="none" />
  </svg>
);

export const IconCoverage: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M8 1.8 13.5 3.6v4.1c0 3.1-2.2 5.6-5.5 6.5-3.3-.9-5.5-3.4-5.5-6.5V3.6z" />
    <path d="M5.7 8.1 7.4 9.8l3-3.4" />
  </svg>
);

export const IconRefresh: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M13.2 8a5.2 5.2 0 1 1-1.7-3.85" />
    <path d="M13.4 2.4v3.1h-3.1" />
  </svg>
);

export const IconChevronRight: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M6 3.5 10.5 8 6 12.5" />
  </svg>
);

export const IconChevronLeft: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M10 3.5 5.5 8 10 12.5" />
  </svg>
);

export const IconChevronDown: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M3.5 6 8 10.5 12.5 6" />
  </svg>
);

export const IconClose: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M4 4l8 8M12 4l-8 8" />
  </svg>
);

export const IconArrowRight: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M3 8h9.5M9 4.5 12.5 8 9 11.5" />
  </svg>
);

export const IconWarning: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M8 2.4 14.2 13H1.8z" />
    <path d="M8 6.4v3.1" />
    <circle cx="8" cy="11.3" r="0.7" fill="currentColor" stroke="none" />
  </svg>
);

export const IconInfo: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <circle cx="8" cy="8" r="6.2" />
    <path d="M8 7.2v4" />
    <circle cx="8" cy="5" r="0.7" fill="currentColor" stroke="none" />
  </svg>
);

export const IconEmpty: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M2.5 4.2h11v8.6h-11z" />
    <path d="M2.5 4.2 4 2.2h8l1.5 2" />
    <path d="M6 7.4h4" />
  </svg>
);

export const IconSearchOff: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <circle cx="7" cy="7" r="4.2" />
    <path d="M10.2 10.2 13.5 13.5" />
    <path d="M5.4 5.4 8.6 8.6M8.6 5.4 5.4 8.6" />
  </svg>
);

export const IconDatabase: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <ellipse cx="8" cy="3.6" rx="5.2" ry="1.9" />
    <path d="M2.8 3.6v8.8c0 1.05 2.33 1.9 5.2 1.9s5.2-.85 5.2-1.9V3.6" />
    <path d="M2.8 8c0 1.05 2.33 1.9 5.2 1.9s5.2-.85 5.2-1.9" />
  </svg>
);

export const IconPulse: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M1.8 8.2h3l1.7-4.4 2.6 8 1.8-3.6h3.3" />
  </svg>
);

export const IconFilter: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M2.4 3.4h11.2L9.4 8.6v4.2L6.6 11V8.6z" />
  </svg>
);

export const IconLayers: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M8 2 14 5.2 8 8.4 2 5.2z" />
    <path d="M2 8.6 8 11.8l6-3.2" />
    <path d="M2 11.8 8 15l6-3.2" />
  </svg>
);

export const IconClock: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <circle cx="8" cy="8" r="6.2" />
    <path d="M8 4.4V8l2.6 1.7" />
  </svg>
);

export const IconExternal: React.FC<IconProps> = ({ size = 14, className }) => (
  <svg {...svgProps(size, className)}>
    <path d="M6.5 3.5H3.2v9.3h9.3V9.5" />
    <path d="M9.2 3.5h3.3v3.3" />
    <path d="M12.5 3.5 7.6 8.4" />
  </svg>
);
