import type { ReactElement } from "react";

interface P { className?: string }
const base = { viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": true, focusable: false } as const;

export const ShieldMark = ({ className = "h-7 w-7" }: P): ReactElement => (
  <svg viewBox="0 0 32 32" className={className} aria-hidden="true" focusable="false">
    <path d="M16 2.5 4.5 7v8.2c0 7 4.8 12.2 11.5 14.3 6.7-2.1 11.5-7.3 11.5-14.3V7L16 2.5Z" fill="#162739" stroke="#d9ad5f" strokeWidth="1.8" strokeLinejoin="round" />
    <path d="M16 8.5v15M9.5 13.5l6.5 3.2 6.5-3.2" stroke="#d9ad5f" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);
export const IconOverview = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><rect x="3" y="3" width="7" height="9" rx="1.5" /><rect x="14" y="3" width="7" height="5" rx="1.5" /><rect x="14" y="12" width="7" height="9" rx="1.5" /><rect x="3" y="16" width="7" height="5" rx="1.5" /></svg>
);
export const IconAlert = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M12 3 2.5 20h19L12 3Z" /><path d="M12 10v4.5M12 17.2v.1" /></svg>
);
export const IconServer = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><rect x="3" y="4" width="18" height="6.5" rx="1.5" /><rect x="3" y="13.5" width="18" height="6.5" rx="1.5" /><path d="M7 7.2v.1M7 16.8v.1" /></svg>
);
export const IconPulse = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M2.5 12h4l2.5-7 4 14 2.5-7h6" /></svg>
);
export const IconBell = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M6 9a6 6 0 0 1 12 0c0 6 2.5 7.5 2.5 7.5h-17S6 15 6 9Z" /><path d="M10 20a2.2 2.2 0 0 0 4 0" /></svg>
);
export const IconBellOff = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M8.5 4.8A6 6 0 0 1 18 9c0 3 .7 4.9 1.4 6M6 9c0 6-2.5 7.5-2.5 7.5H15" /><path d="M10 20a2.2 2.2 0 0 0 4 0M3 3l18 18" /></svg>
);
export const IconLogout = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M10 4H5.5A1.5 1.5 0 0 0 4 5.5v13A1.5 1.5 0 0 0 5.5 20H10M15 8l4 4-4 4M19 12H9" /></svg>
);
export const IconBack = ({ className = "h-4 w-4" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M15 5l-7 7 7 7" /></svg>
);
export const IconCopy = ({ className = "h-4 w-4" }: P): ReactElement => (
  <svg {...base} className={className}><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V6a2 2 0 0 1 2-2h8" /></svg>
);
export const IconCheck = ({ className = "h-5 w-5" }: P): ReactElement => (
  <svg {...base} className={className}><path d="M4.5 12.5l5 5 10-11" /></svg>
);
