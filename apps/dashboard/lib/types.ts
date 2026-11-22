export type Role = "admin" | "analyst";
export type Severity = "info" | "low" | "medium" | "high" | "critical";
export type IncidentStatus = "open" | "investigating" | "resolved" | "closed";
export type ApprovalAction = "isolate_host" | "block_ip" | "disable_account";

export interface Me { subject: string; role: Role; tenant_id: string }

export interface Incident {
  id: string; title: string; severity: Severity; status: IncidentStatus; rule_id: string;
  owner_id: string | null; created_at: string; updated_at: string; event_count: number;
}
export interface EventRow {
  id: string; agent_id: string; occurred_at: string; received_at?: string;
  kind: string; severity: Severity; payload: unknown;
}
export interface Agent {
  id: string; hostname: string; os: string; last_seen_at: string | null; status: "active" | "revoked";
}
export interface Alert {
  id: string; rule_id: string; severity: Severity; status: string; event_id: string; agent_id: string; created_at: string;
}
export interface Approval {
  id: string; action: string; decision: "pending" | "approved" | "denied"; requester: string; approver: string;
  created_at: string; decided_at: string | null; executed_at: string | null; simulation_note: string;
}
export interface AuditRow { at: string; actor: string; action: string; result: string; metadata: unknown }
export interface IncidentDetail {
  incident: Incident; events: EventRow[]; alerts: Alert[]; approvals: Approval[]; audit_trail: AuditRow[];
}
export interface LiveMessage { type: string; data: Record<string, unknown> }

export const TENANTS = [
  { id: "11111111-1111-1111-1111-111111111111", name: "Lab Tenant A" },
  { id: "22222222-2222-2222-2222-222222222222", name: "Lab Tenant B" },
];
export const STATUSES: IncidentStatus[] = ["open", "investigating", "resolved", "closed"];
// Mirrors the server's rules, only to decide which buttons to SHOW. The API enforces them.
export const TRANSITIONS: Record<IncidentStatus, IncidentStatus[]> = {
  open: ["investigating", "resolved"],
  investigating: ["open", "resolved"],
  resolved: ["investigating", "closed"],
  closed: [],
};
export const ACTIONS: Record<ApprovalAction, string> = {
  isolate_host: "Isolate host (simulation)",
  block_ip: "Block IP (simulation)",
  disable_account: "Disable account (simulation)",
};
