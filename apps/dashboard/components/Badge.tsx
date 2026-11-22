import type { Severity } from "../lib/types";

// Full class names are written out so Tailwind can see them. Unknown values fall back to a neutral style.
const SEV: Record<Severity, string> = {
  critical: "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300",
  high: "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300",
  medium: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  low: "bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
  info: "bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
};
const GOOD = "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300";
const WARN = "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300";
const BAD = "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300";
const NEUTRAL = "bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300";
const STATE: Record<string, string> = {
  open: GOOD, active: GOOD, online: GOOD, approved: GOOD,
  investigating: WARN, late: WARN, pending: WARN,
  revoked: BAD, offline: BAD, denied: BAD,
  resolved: NEUTRAL, closed: NEUTRAL, never: NEUTRAL,
};
const BASE = "inline-block rounded-full px-2 py-0.5 text-xs font-semibold";

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`${BASE} ${SEV[severity] ?? SEV.info}`}>{severity}</span>;
}

export function StateBadge({ value }: { value: string }) {
  return <span className={`${BASE} ${STATE[value] ?? NEUTRAL}`}>{value}</span>;
}
