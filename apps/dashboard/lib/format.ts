import type { Agent } from "./types";

export const fmt = (iso: string | null | undefined): string => (iso ? new Date(iso).toLocaleString() : "—");

export function ago(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return "never";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s} s ago`;
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  return `${Math.round(s / 3600)} h ago`;
}

export type AgentState = "revoked" | "never" | "online" | "late" | "offline";
export function agentState(a: Agent, now: number = Date.now()): AgentState {
  if (a.status === "revoked") return "revoked";
  if (!a.last_seen_at) return "never";
  const s = (now - new Date(a.last_seen_at).getTime()) / 1000;
  return s < 90 ? "online" : s < 180 ? "late" : "offline";
}

export function short(v: unknown, n: number): string {
  const s = typeof v === "string" ? v : JSON.stringify(v) ?? "";
  return s.length > n ? `${s.slice(0, n)}…` : s;
}

/** Clock time only, for dense lists such as the event feed (24 h, local). */
export const clock = (iso: string | null | undefined): string => (iso ? new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }) : "—");
