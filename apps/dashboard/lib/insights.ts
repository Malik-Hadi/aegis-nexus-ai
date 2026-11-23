import type { EventRow, Incident, Severity } from "./types";

export const SEV_ORDER: Severity[] = ["critical", "high", "medium", "low", "info"];
const RANK: Record<Severity, number> = { info: 0, low: 1, medium: 2, high: 3, critical: 4 };

export const isSeverity = (v: unknown): v is Severity => typeof v === "string" && v in RANK;
export const sevOf = (v: unknown): Severity => (isSeverity(v) ? v : "info");
export const rank = (s: Severity): number => RANK[s] ?? 0;

export interface Bucket { start: number; end: number; count: number; top: Severity }

/** Splits the last `windowMin` minutes into equal buckets and counts events and their highest severity in each. */
export function bucketEvents(events: EventRow[], now: number, windowMin = 60, bucketMin = 2): Bucket[] {
  const size = bucketMin * 60_000;
  const n = Math.round(windowMin / bucketMin);
  const first = now - n * size;
  const buckets: Bucket[] = Array.from({ length: n }, (_, i) => ({ start: first + i * size, end: first + (i + 1) * size, count: 0, top: "info" as Severity }));
  for (const e of events) {
    const t = new Date(e.occurred_at).getTime();
    if (!Number.isFinite(t) || t < first || t > now) continue;
    const b = buckets[Math.min(n - 1, Math.floor((t - first) / size))];
    b.count += 1;
    const s = sevOf(e.severity);
    if (rank(s) > rank(b.top)) b.top = s;
  }
  return buckets;
}

/** Events per minute over the last `minutes`. `capped` is true when the loaded list is full and may hide older events in range. */
export function eventsPerMinute(events: EventRow[], now: number, minutes = 5, loadLimit = 100): { value: number; capped: boolean } {
  const from = now - minutes * 60_000;
  let n = 0;
  let oldest = Infinity;
  for (const e of events) {
    const t = new Date(e.occurred_at).getTime();
    if (!Number.isFinite(t)) continue;
    if (t < oldest) oldest = t;
    if (t >= from && t <= now) n += 1;
  }
  const capped = events.length >= loadLimit && oldest > from;
  return { value: Math.round((n / minutes) * 10) / 10, capped };
}

export function severityCounts(incidents: Incident[]): Record<Severity, number> {
  const out: Record<Severity, number> = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  for (const i of incidents) out[sevOf(i.severity)] += 1;
  return out;
}

export const isActive = (i: Incident): boolean => i.status === "open" || i.status === "investigating";
