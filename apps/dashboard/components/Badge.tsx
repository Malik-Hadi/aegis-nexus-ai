import type { Severity } from "../lib/types";

const GOOD = "st-good";
const WARN = "st-warn";
const BAD = "st-bad";
const NEUTRAL = "st-neutral";
const STATE: Record<string, string> = {
  open: BAD, active: GOOD, online: GOOD, approved: GOOD,
  investigating: WARN, late: WARN, pending: WARN,
  revoked: BAD, offline: BAD, denied: BAD,
  resolved: GOOD, closed: NEUTRAL, never: NEUTRAL,
};
const SEV_KNOWN = new Set(["critical", "high", "medium", "low", "info"]);

export function SeverityBadge({ severity }: { severity: Severity }) {
  const key = SEV_KNOWN.has(severity) ? severity : "info";
  return <span className={`chip sev-${key}`}>{severity}</span>;
}

export function StateBadge({ value }: { value: string }) {
  return <span className={`chip ${STATE[value] ?? NEUTRAL}`}>{value}</span>;
}

/** A small coloured dot for dense lists. The colour is never the only signal: the text sits beside it. */
export function SevDot({ severity }: { severity: Severity }) {
  const key = SEV_KNOWN.has(severity) ? severity : "info";
  return <span className={`dot sev-${key}`} aria-hidden="true" />;
}
