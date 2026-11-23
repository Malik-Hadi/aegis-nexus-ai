'use client';
import { useCallback, useEffect, useRef, useState } from "react";
import AgentsView from "../components/AgentsView";
import EventsView from "../components/EventsView";
import Header from "../components/Header";
import Nav from "../components/Nav";
import type { Tab } from "../components/Nav";
import Overview from "../components/Overview";
import { ErrorNote } from "../components/States";
import { ShieldMark } from "../components/Icons";
import IncidentDetailView from "../components/IncidentDetailView";
import IncidentsView from "../components/IncidentsView";
import LoginForm from "../components/LoginForm";
import Toasts from "../components/Toasts";
import type { ToastItem } from "../components/Toasts";
import { api, setUnauthorizedHandler } from "../lib/api";
import type { ApiResult } from "../lib/api";
import { useLive } from "../lib/useLive";
import { armSound, playAlert, readSoundPref, saveSoundPref, useNow } from "../lib/hooks";
import { isActive } from "../lib/insights";
import type { LiveMessage } from "../lib/types";
import type {
  Agent, ApprovalAction, EventRow, Incident, IncidentDetail, IncidentStatus, Me, Severity,
} from "../lib/types";

const TITLES: Record<Tab, string> = { overview: "Overview", incidents: "Incidents", agents: "Agents", events: "Event feed" };
const SEVERITIES: string[] = ["info", "low", "medium", "high", "critical"];
const asSeverity = (v: unknown): Severity => (typeof v === "string" && SEVERITIES.includes(v) ? (v as Severity) : "info");
const asString = (v: unknown): string => (typeof v === "string" ? v : "");

// The server enforces tenants, roles and rules on every call. This page only decides what to SHOW.
export default function Home() {
  const [booting, setBooting] = useState(true);
  const [me, setMe] = useState<Me | null>(null);
  const [loginError, setLoginError] = useState("");
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<Tab>("overview");
  const [loaded, setLoaded] = useState({ incidents: false, agents: false, events: false });
  const [soundOn, setSoundOn] = useState(false);
  const soundRef = useRef(false);
  const now = useNow(15_000);
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

  useEffect(() => { const on = readSoundPref(); setSoundOn(on); soundRef.current = on; }, []);
  const toggleSound = () => {
    const on = !soundOn;
    if (on) { armSound(); playAlert(); }
    setSoundOn(on); soundRef.current = on; saveSoundPref(on);
  };

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
    if (generation !== sessionGeneration.current) return;
    if (r.ok && r.data) setIncidents(r.data.incidents);
    setLoaded((l) => ({ ...l, incidents: true }));
  }, [filter]);
  const loadAgents = useCallback(async () => {
    const generation = sessionGeneration.current;
    const r = await api<{ agents: Agent[] }>("GET", "/v1/agents");
    if (generation !== sessionGeneration.current) return;
    if (r.ok && r.data) setAgents(r.data.agents);
    setLoaded((l) => ({ ...l, agents: true }));
  }, []);
  const loadEvents = useCallback(async () => {
    const generation = sessionGeneration.current;
    const r = await api<{ events: EventRow[] }>("GET", "/v1/events?limit=100");
    if (generation !== sessionGeneration.current) return;
    if (r.ok && r.data) setEvents(r.data.events);
    setLoaded((l) => ({ ...l, events: true }));
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
    setTab("overview"); setFilter(""); setError("");
    setLoaded({ incidents: false, agents: false, events: false });
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
          if (soundRef.current && (d.severity === "high" || d.severity === "critical")) playAlert();
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

  const goTo = (t: Tab) => { setTab(t); setError(""); back(); };
  const openFromOverview = (id: string) => { setTab("incidents"); openIncident(id); };
  const activeCount = incidents.filter(isActive).length;

  // ---------- render ----------
  return (
    <>
      <Toasts toasts={toasts} />
      {booting ? (
        <div className="flex min-h-screen flex-col items-center justify-center gap-4" aria-busy="true">
          <ShieldMark className="h-12 w-12" />
          <p className="text-sm font-semibold tracking-wide">AEGIS NEXUS AI</p>
          <p className="text-sm text-[#8fa3b8]">Loading…</p>
        </div>
      ) : !me ? (
        <LoginForm error={loginError} busy={busy} onSubmit={login} />
      ) : (
        <div className="min-h-screen md:grid md:grid-cols-[15rem_minmax(0,1fr)]">
          <Nav tab={tab} onTab={goTo} activeIncidents={activeCount} />
          <div className="flex min-w-0 flex-col pb-20 md:pb-0">
            <Header me={me} status={live} title={TITLES[tab]} soundOn={soundOn} onSound={toggleSound} onLogout={logout} />
            <main className="mx-auto w-full max-w-7xl p-4 md:p-6">
              {error && tab !== "incidents" ? <ErrorNote>{error}</ErrorNote> : null}
              {tab === "overview" ? (
                <Overview incidents={incidents} agents={agents} events={events} now={now} loaded={loaded}
                  isAdmin={me.role === "admin"} onOpen={openFromOverview} onGo={goTo} />
              ) : null}
              {tab === "incidents" ? (
                selected && detail ? (
                  <IncidentDetailView key={selected}
                    detail={detail} me={me} error={error}
                    onBack={back} onStatus={setStatus} onAssignMe={assignMe} onRequest={requestApproval} onDecide={decide}
                  />
                ) : (
                  <IncidentsView incidents={incidents} loaded={loaded.incidents} now={now} filter={filter} onFilter={setFilter} onOpen={openIncident} />
                )
              ) : null}
              {tab === "agents" ? (
                <AgentsView agents={agents} loaded={loaded.agents} now={now} isAdmin={me.role === "admin"} newToken={newToken} onCreateToken={createToken} onRevoke={revoke} />
              ) : null}
              {tab === "events" ? <EventsView events={events} loaded={loaded.events} /> : null}
            </main>
          </div>
        </div>
      )}
    </>
  );
}
