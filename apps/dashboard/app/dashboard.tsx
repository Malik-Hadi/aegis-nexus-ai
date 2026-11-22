'use client';
import { useCallback, useEffect, useRef, useState } from "react";
import AgentsView from "../components/AgentsView";
import EventsView from "../components/EventsView";
import Header from "../components/Header";
import IncidentDetailView from "../components/IncidentDetailView";
import IncidentsView from "../components/IncidentsView";
import LoginForm from "../components/LoginForm";
import Toasts from "../components/Toasts";
import type { ToastItem } from "../components/Toasts";
import { api, setUnauthorizedHandler } from "../lib/api";
import type { ApiResult } from "../lib/api";
import { useLive } from "../lib/useLive";
import type { LiveMessage } from "../lib/types";
import type {
  Agent, ApprovalAction, EventRow, Incident, IncidentDetail, IncidentStatus, Me, Severity,
} from "../lib/types";

type Tab = "incidents" | "agents" | "events";
const TABS: { key: Tab; label: string }[] = [
  { key: "incidents", label: "Incidents" },
  { key: "agents", label: "Agents" },
  { key: "events", label: "Event feed" },
];
const SEVERITIES: string[] = ["info", "low", "medium", "high", "critical"];
const asSeverity = (v: unknown): Severity => (typeof v === "string" && SEVERITIES.includes(v) ? (v as Severity) : "info");
const asString = (v: unknown): string => (typeof v === "string" ? v : "");

// The server enforces tenants, roles and rules on every call. This page only decides what to SHOW.
export default function Home() {
  const [booting, setBooting] = useState(true);
  const [me, setMe] = useState<Me | null>(null);
  const [loginError, setLoginError] = useState("");
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<Tab>("incidents");
  const [filter, setFilter] = useState<IncidentStatus | "">("");
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [events, setEvents] = useState<EventRow[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<IncidentDetail | null>(null);
  const [error, setError] = useState("");
  const [newToken, setNewToken] = useState<string | null>(null);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const toastId = useRef(0);
  const sessionGeneration = useRef(0);

  const schedule = useCallback((name: string, fn: () => void) => {
    const old = timers.current[name];
    if (old) clearTimeout(old);
    timers.current[name] = setTimeout(fn, 0);
  }, []);

  const toast = useCallback((text: string, severity: Severity) => {
    toastId.current += 1;
    const id = toastId.current;
    setToasts((l) => [...l, { id, text, severity }]);
    setTimeout(() => setToasts((l) => l.filter((x) => x.id !== id)), 7000);
  }, []);

  // ---------- data loading ----------
  const loadIncidents = useCallback(async () => {
    const generation = sessionGeneration.current;
    const q = `/v1/incidents?limit=100${filter ? `&status=${encodeURIComponent(filter)}` : ""}`;
    const r = await api<{ incidents: Incident[] }>("GET", q);
    if (generation === sessionGeneration.current && r.ok && r.data) setIncidents(r.data.incidents);
  }, [filter]);
  const loadAgents = useCallback(async () => {
    const generation = sessionGeneration.current;
    const r = await api<{ agents: Agent[] }>("GET", "/v1/agents");
    if (generation === sessionGeneration.current && r.ok && r.data) setAgents(r.data.agents);
  }, []);
  const loadEvents = useCallback(async () => {
    const generation = sessionGeneration.current;
    const r = await api<{ events: EventRow[] }>("GET", "/v1/events?limit=100");
    if (generation === sessionGeneration.current && r.ok && r.data) setEvents(r.data.events);
  }, []);
  const loadDetail = useCallback(async (id: string) => {
    const generation = sessionGeneration.current;
    const r = await api<IncidentDetail>("GET", `/v1/incidents/${encodeURIComponent(id)}`);
    if (generation !== sessionGeneration.current) return;
    if (r.ok && r.data) { setDetail(r.data); setSelected(id); }
    else if (r.status === 404) { setDetail(null); setSelected(null); }
  }, []);
  const reloadAll = useCallback(async () => {
    await Promise.all([loadIncidents(), loadAgents(), loadEvents()]);
    if (selected) await loadDetail(selected);
  }, [loadIncidents, loadAgents, loadEvents, loadDetail, selected]);

  // ---------- session ----------
  const clearSession = useCallback(() => {
    sessionGeneration.current++;
    for (const timer of Object.values(timers.current)) clearTimeout(timer);
    timers.current = {};
    setIncidents([]); setAgents([]); setEvents([]); setToasts([]);
    setMe(null); setDetail(null); setSelected(null); setNewToken(null);
    setTab("incidents"); setFilter(""); setError("");
  }, []);

  const sessionEnded = useCallback(() => {
    clearSession();
    setLoginError("Your session ended. Please sign in again.");
  }, [clearSession]);

  useEffect(() => {
    let alive = true;
    api<Me>("GET", "/v1/me").then((r) => {
      if (!alive) return;
      if (r.ok && r.data) setMe(r.data);
      setBooting(false);
    });
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    if (!me) return undefined;
    setUnauthorizedHandler(() => sessionEnded());
    void Promise.all([loadAgents(), loadEvents()]);
    return () => setUnauthorizedHandler(null);
  }, [me, sessionEnded, loadAgents, loadEvents]);

  useEffect(() => { if (me) void loadIncidents(); }, [me, loadIncidents]);

  const login = async (tenantId: string, subject: string, password: string) => {
    setBusy(true); setLoginError("");
    const r = await api("POST", "/v1/auth/login", { tenant_id: tenantId, subject, password });
    if (!r.ok) {
      setLoginError(r.status === 401 ? "Wrong tenant, user or password." : `Sign-in failed (${r.status}).`);
      setBusy(false);
      return;
    }
    const m = await api<Me>("GET", "/v1/me");
    setBusy(false);
    if (m.ok && m.data) setMe(m.data); else setLoginError("Could not load your profile.");
  };
  const logout = async () => {
    await api("POST", "/v1/auth/logout");
    clearSession(); setLoginError("");
  };

  // ---------- real-time ----------
  const live = useLive(me !== null, {
    onMessage: (m: LiveMessage) => {
      const d = m.data;
      const id = asString(d.id);
      switch (m.type) {
        case "event.new": schedule("events", () => void loadEvents()); break;
        case "incident.created":
          toast(`New ${asString(d.severity)} incident: ${asString(d.title) || asString(d.rule_id)}`, asSeverity(d.severity));
          schedule("incidents", () => void loadIncidents());
          break;
        case "incident.updated":
          schedule("incidents", () => void loadIncidents());
          if (selected && selected === id) schedule("detail", () => void loadDetail(id));
          break;
        case "agent.heartbeat": case "agent.revoked": schedule("agents", () => void loadAgents()); break;
        case "approval.requested": case "approval.decided": {
          toast(m.type === "approval.requested" ? "Approval requested" : `Approval ${asString(d.decision) || "decided"}`, "info");
          const inc = asString(d.incident_id);
          if (selected && selected === inc) schedule("detail", () => void loadDetail(inc));
          break;
        }
        default: break;
      }
    },
    onResync: () => { void reloadAll(); },
    onCatchUp: () => { void reloadAll(); },
    onExpired: sessionEnded,
  });

  // ---------- actions ----------
  const run = async (p: Promise<ApiResult<unknown>>, okText?: string): Promise<void> => {
    const r = await p;
    if (r.ok) { setError(""); if (okText) toast(okText, "info"); } else setError(r.error);
  };
  const setStatus = async (to: IncidentStatus) => {
    if (!selected) return;
    await run(api("PATCH", `/v1/incidents/${selected}`, { status: to }), `Status: ${to}`);
    await loadDetail(selected); void loadIncidents();
  };
  const assignMe = async () => {
    if (!selected) return;
    await run(api("PATCH", `/v1/incidents/${selected}`, { owner: "me" }), "Assigned to you");
    await loadDetail(selected);
  };
  const requestApproval = async (action: ApprovalAction) => {
    if (!selected) return;
    await run(api("POST", `/v1/incidents/${selected}/approvals`, { action }), "Approval requested");
    await loadDetail(selected);
  };
  const decide = async (approvalId: string, decision: "approved" | "denied") => {
    if (!selected) return;
    await run(api("POST", `/v1/approvals/${approvalId}/decision`, { decision }), `Decision: ${decision}`);
    await loadDetail(selected);
  };
  const revoke = async (a: Agent) => {
    if (!window.confirm(`Revoke ${a.hostname}? It will be rejected immediately.`)) return;
    await run(api("POST", `/v1/agents/${a.id}/revoke`), "Agent revoked");
    await loadAgents();
  };
  const createToken = async () => {
    const r = await api<{ enrolment_token: string }>("POST", "/v1/admin/enrolment-tokens");
    if (r.ok && r.data) { setNewToken(r.data.enrolment_token); setError(""); } else { setNewToken(null); setError(r.error); }
  };
  const openIncident = (id: string) => { setError(""); void loadDetail(id); };
  const back = () => { setSelected(null); setDetail(null); setError(""); };

  // ---------- render ----------
  return (
    <>
      <Toasts toasts={toasts} />
      {booting ? (
        <p className="p-6 text-sm text-slate-500">Loading…</p>
      ) : !me ? (
        <LoginForm error={loginError} busy={busy} onSubmit={login} />
      ) : (
        <>
          <Header me={me} status={live} onLogout={logout} />
          <nav className="flex gap-1 overflow-x-auto border-b border-slate-200 bg-white px-5 pt-2 dark:border-slate-800 dark:bg-slate-900">
            {TABS.map((t) => (
              <button
                key={t.key}
                type="button"
                onClick={() => { setTab(t.key); setError(""); back(); }}
                className={`border-b-4 px-4 py-2 text-sm ${tab === t.key ? "border-blue-600 font-semibold" : "border-transparent text-slate-500"}`}
              >
                {t.label}
              </button>
            ))}
          </nav>
          <main className="mx-auto max-w-6xl p-5">
            {error && tab !== "incidents" ? <div role="alert" className="mb-3 rounded-md bg-red-100 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{error}</div> : null}
            {tab === "incidents" ? (
              selected && detail ? (
                <IncidentDetailView key={selected}
                  detail={detail} me={me} error={error}
                  onBack={back} onStatus={setStatus} onAssignMe={assignMe} onRequest={requestApproval} onDecide={decide}
                />
              ) : (
                <IncidentsView incidents={incidents} agents={agents} events={events} filter={filter} onFilter={setFilter} onOpen={openIncident} />
              )
            ) : null}
            {tab === "agents" ? (
              <AgentsView agents={agents} isAdmin={me.role === "admin"} newToken={newToken} onCreateToken={createToken} onRevoke={revoke} />
            ) : null}
            {tab === "events" ? <EventsView events={events} /> : null}
          </main>
        </>
      )}
    </>
  );
}
