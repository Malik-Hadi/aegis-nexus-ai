"use strict";
// AEGIS NEXUS AI console. Security rules for this file:
//  - NEVER turn text into HTML or run dynamic code: events and payloads are untrusted text.
//  - All dynamic text goes through el() which uses textContent / text nodes only.
//  - Tenant and role come from the server (/v1/me); this file only decides what to SHOW. The API enforces everything.

const TENANTS = [
  { id: "11111111-1111-1111-1111-111111111111", name: "Lab Tenant A" },
  { id: "22222222-2222-2222-2222-222222222222", name: "Lab Tenant B" },
];
const TRANSITIONS = {
  open: ["investigating", "resolved"],
  investigating: ["open", "resolved"],
  resolved: ["investigating", "closed"],
  closed: [],
};
const ACTIONS = { isolate_host: "Isolate host (simulation)", block_ip: "Block IP (simulation)", disable_account: "Disable account (simulation)" };
const SEVERITIES = ["info", "low", "medium", "high", "critical"];
const STATUSES = ["open", "investigating", "resolved", "closed"];

const state = {
  me: null, tab: "incidents", statusFilter: "", incidents: [], agents: [], events: [],
  selected: null, detail: null, stream: null, wasDown: false, newToken: null, error: "", loginError: "",
};
const timers = {};
const root = document.getElementById("app");

// ---------- helpers ----------
function el(tag, props, ...kids) {
  const n = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v == null || v === false) continue;
      if (k === "class") n.className = v;
      else if (k === "text") n.textContent = v;
      else if (k.startsWith("on") && typeof v === "function") n.addEventListener(k.slice(2), v);
      else if (v === true) n.setAttribute(k, "");
      else n.setAttribute(k, String(v));
    }
  }
  for (const c of kids.flat()) {
    if (c == null || c === false) continue;
    n.append(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return n;
}
const pick = (v, list, fallback) => (list.includes(v) ? v : fallback);
const sevBadge = (s) => el("span", { class: "b sev-" + pick(s, SEVERITIES, "info"), text: String(s || "") });
const stBadge = (s) => el("span", { class: "b st-" + pick(s, STATUSES.concat(["active", "revoked", "online", "late", "never", "offline"]), "open"), text: String(s || "") });
const fmt = (iso) => (iso ? new Date(iso).toLocaleString() : "—");
function ago(iso) {
  if (!iso) return "never";
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return s + " s ago";
  if (s < 3600) return Math.round(s / 60) + " min ago";
  return Math.round(s / 3600) + " h ago";
}
function agentState(a) {
  if (a.status === "revoked") return "revoked";
  if (!a.last_seen_at) return "never";
  const s = (Date.now() - new Date(a.last_seen_at).getTime()) / 1000;
  return s < 90 ? "online" : s < 180 ? "late" : "offline";
}
const short = (v, n) => { const s = typeof v === "string" ? v : JSON.stringify(v); return s.length > n ? s.slice(0, n) + "…" : s; };
const isAdmin = () => state.me && state.me.role === "admin";

function schedule(name, fn, ms) { clearTimeout(timers[name]); timers[name] = setTimeout(fn, ms); }

function toast(text, sev) {
  const t = el("div", { class: "toast sev-" + pick(sev, SEVERITIES, "info"), text });
  document.getElementById("toasts").append(t);
  setTimeout(() => t.remove(), 7000);
}

async function api(method, path, body) {
  const opt = { method, credentials: "same-origin", headers: {} };
  if (body !== undefined) { opt.headers["Content-Type"] = "application/json"; opt.body = JSON.stringify(body); }
  let r;
  try { r = await fetch(path, opt); } catch { return { ok: false, status: 0, data: { error: "network error" } }; }
  let data = null;
  try { data = await r.json(); } catch { data = null; }
  if (r.status === 401 && state.me) sessionEnded();
  return { ok: r.ok, status: r.status, data: data || {} };
}

// ---------- session ----------
async function boot() {
  const r = await fetch("/v1/me", { credentials: "same-origin" }).catch(() => null);
  if (r && r.ok) { state.me = await r.json(); await enterApp(); } else renderLogin();
}
async function enterApp() { render(); await reloadAll(); startStream(); }
function sessionEnded() {
  stopStream();
  state.me = null; state.detail = null; state.selected = null;
  state.loginError = "Your session ended. Please sign in again.";
  renderLogin();
}
async function login(ev) {
  ev.preventDefault();
  const f = ev.target;
  const r = await api("POST", "/v1/auth/login", { tenant_id: f.tenant.value, subject: f.subject.value.trim(), password: f.password.value });
  if (!r.ok) { state.loginError = r.status === 401 ? "Wrong tenant, user or password." : "Sign-in failed (" + r.status + ")."; renderLogin(); return; }
  f.password.value = "";
  const me = await api("GET", "/v1/me");
  if (!me.ok) { state.loginError = "Could not load your profile."; renderLogin(); return; }
  state.me = me.data; state.loginError = "";
  await enterApp();
}
async function logout() { await api("POST", "/v1/auth/logout"); stopStream(); state.me = null; state.loginError = ""; renderLogin(); }

// ---------- data ----------
async function loadIncidents() {
  const q = "/v1/incidents?limit=100" + (state.statusFilter ? "&status=" + encodeURIComponent(state.statusFilter) : "");
  const r = await api("GET", q);
  if (r.ok) state.incidents = r.data.incidents || [];
  render();
}
async function loadAgents() { const r = await api("GET", "/v1/agents"); if (r.ok) state.agents = r.data.agents || []; render(); }
async function loadEvents() { const r = await api("GET", "/v1/events?limit=100"); if (r.ok) state.events = r.data.events || []; render(); }
async function loadDetail(id) {
  const r = await api("GET", "/v1/incidents/" + encodeURIComponent(id));
  if (r.ok) { state.detail = r.data; state.selected = id; } else if (r.status === 404) { state.detail = null; state.selected = null; }
  render();
}
async function reloadAll() {
  await Promise.all([loadIncidents(), loadAgents(), loadEvents()]);
  if (state.selected) await loadDetail(state.selected);
}

// ---------- real-time stream (Server-Sent Events) ----------
function setLive(text, cls) {
  const n = document.getElementById("live");
  if (n) { n.textContent = text; n.className = "live " + cls; }
}
function stopStream() { if (state.stream) { state.stream.close(); state.stream = null; } }
function startStream() {
  stopStream();
  const es = new EventSource("/v1/stream");
  state.stream = es;
  es.onopen = () => { setLive("Live", "on"); if (state.wasDown) { state.wasDown = false; reloadAll(); } };
  es.onmessage = (e) => { try { onLive(JSON.parse(e.data)); } catch { /* ignore malformed */ } };
  es.addEventListener("resync", () => reloadAll());
  es.addEventListener("expired", () => sessionEnded());
  es.onerror = async () => {
    state.wasDown = true;
    setLive("Reconnecting…", "off");
    if (es.readyState === EventSource.CLOSED) {
      const r = await api("GET", "/v1/me");
      if (r.ok && state.me) setTimeout(startStream, 3000);
    }
  };
}
function onLive(m) {
  const d = m.data || {};
  switch (m.type) {
    case "event.new": schedule("events", loadEvents, 300); break;
    case "incident.created":
      toast("New " + (d.severity || "") + " incident: " + (d.title || d.rule_id || ""), d.severity);
      schedule("incidents", loadIncidents, 200);
      break;
    case "incident.updated":
      schedule("incidents", loadIncidents, 300);
      if (state.selected === d.id) schedule("detail", () => loadDetail(d.id), 300);
      break;
    case "agent.heartbeat": case "agent.revoked": schedule("agents", loadAgents, 500); break;
    case "approval.requested": case "approval.decided":
      toast("Approval " + (m.type === "approval.requested" ? "requested" : (d.decision || "decided")), "info");
      if (state.selected === d.incident_id) schedule("detail", () => loadDetail(d.incident_id), 200);
      break;
    default: break;
  }
}

// ---------- actions ----------
function say(r, okText) {
  if (r.ok) { state.error = ""; if (okText) toast(okText, "info"); } else state.error = (r.data && r.data.error) || "request failed (" + r.status + ")";
}
async function setStatus(to) { say(await api("PATCH", "/v1/incidents/" + state.selected, { status: to }), "Status: " + to); await loadDetail(state.selected); loadIncidents(); }
async function assignMe() { say(await api("PATCH", "/v1/incidents/" + state.selected, { owner: "me" }), "Assigned to you"); await loadDetail(state.selected); }
async function requestApproval(action) { say(await api("POST", "/v1/incidents/" + state.selected + "/approvals", { action }), "Approval requested"); await loadDetail(state.selected); }
async function decide(id, decision) { say(await api("POST", "/v1/approvals/" + id + "/decision", { decision }), "Decision: " + decision); await loadDetail(state.selected); }
async function revokeAgent(a) {
  if (!window.confirm("Revoke " + a.hostname + "? It will be rejected immediately.")) return;
  say(await api("POST", "/v1/agents/" + a.id + "/revoke"), "Agent revoked"); await loadAgents();
}
async function createToken() {
  const r = await api("POST", "/v1/admin/enrolment-tokens");
  state.newToken = r.ok ? r.data.enrolment_token : null; say(r); render();
}

// ---------- views ----------
function renderLogin() {
  const form = el("form", { class: "login", onsubmit: login },
    el("div", { class: "brand", text: "AEGIS NEXUS AI" }),
    el("p", { class: "muted", text: "SOC console – lab environment" }),
    state.loginError ? el("div", { class: "msg", text: state.loginError }) : null,
    el("label", { for: "tenant", text: "Tenant" }),
    el("select", { id: "tenant", name: "tenant" }, TENANTS.map((t) => el("option", { value: t.id, text: t.name }))),
    el("label", { for: "subject", text: "User" }),
    el("input", { id: "subject", name: "subject", autocomplete: "username", required: true }),
    el("label", { for: "password", text: "Password" }),
    el("input", { id: "password", name: "password", type: "password", autocomplete: "current-password", required: true }),
    el("button", { class: "p", type: "submit", text: "Sign in" }));
  root.replaceChildren(form);
}

function render() {
  if (!state.me) return;
  const tabs = [["incidents", "Incidents"], ["agents", "Agents"], ["events", "Event feed"]];
  const head = el("header", null,
    el("div", { class: "brand", text: "AEGIS NEXUS AI" }),
    el("span", { id: "live", class: "live off", text: state.stream ? "Connecting…" : "Offline" }),
    el("div", { class: "who right", text: state.me.subject + " (" + state.me.role + ") · " + (TENANTS.find((t) => t.id === state.me.tenant_id) || { name: "tenant" }).name }),
    el("button", { onclick: logout, text: "Sign out" }));
  const nav = el("nav", null, tabs.map(([k, label]) => el("button", { class: state.tab === k ? "on" : "", onclick: () => { state.tab = k; state.selected = null; state.detail = null; state.error = ""; render(); }, text: label })));
  const main = el("main", null, state.error ? el("div", { class: "msg", text: state.error }) : null,
    state.tab === "incidents" ? (state.selected && state.detail ? viewDetail() : viewIncidents()) : state.tab === "agents" ? viewAgents() : viewEvents());
  root.replaceChildren(head, nav, main);
  if (state.stream && state.stream.readyState === EventSource.OPEN) setLive("Live", "on");
}

function viewIncidents() {
  const open = state.incidents.filter((i) => i.status === "open" || i.status === "investigating");
  const high = open.filter((i) => i.severity === "high" || i.severity === "critical");
  const online = state.agents.filter((a) => agentState(a) === "online").length;
  const filter = el("select", { onchange: (e) => { state.statusFilter = e.target.value; loadIncidents(); } },
    el("option", { value: "", text: "All statuses" }), STATUSES.map((s) => el("option", { value: s, text: s, selected: state.statusFilter === s })));
  return el("div", null,
    el("div", { class: "cards" },
      stat(open.length, "Active incidents"), stat(high.length, "High severity active"), stat(online, "Agents online"), stat(state.events.length, "Recent events")),
    el("div", { class: "card" },
      el("div", { class: "row-actions" }, el("h2", { text: "Incidents" }), el("span", { class: "right" }, filter)),
      state.incidents.length === 0 ? el("p", { class: "muted", text: "No incidents yet. They appear here live when a rule fires." }) :
        el("div", { class: "tw" }, el("table", null,
          el("tr", null, ["Created", "Title", "Rule", "Severity", "Status", "Evidence"].map((h) => el("th", { text: h }))),
          state.incidents.map((i) => el("tr", { class: "row", onclick: () => loadDetail(i.id) },
            el("td", { text: fmt(i.created_at) }), el("td", { text: i.title }), el("td", { text: i.rule_id }),
            el("td", null, sevBadge(i.severity)), el("td", null, stBadge(i.status)), el("td", { text: String(i.event_count) })))))));
}
const stat = (n, label) => el("div", { class: "stat" }, el("b", { text: String(n) }), el("span", { text: label }));

function viewDetail() {
  const d = state.detail, i = d.incident;
  const next = (TRANSITIONS[i.status] || []).filter((to) => to !== "closed" || isAdmin());
  const active = i.status === "open" || i.status === "investigating";
  return el("div", { class: "card" },
    el("button", { onclick: () => { state.selected = null; state.detail = null; state.error = ""; render(); }, text: "← Back" }),
    el("h2", { text: i.title }),
    el("p", null, sevBadge(i.severity), " ", stBadge(i.status), " Rule ", el("code", { text: i.rule_id }), " · created ", fmt(i.created_at), " · owner ", i.owner_id ? el("code", { text: i.owner_id.slice(0, 8) }) : "unassigned"),
    el("div", { class: "row-actions" },
      next.map((to) => el("button", { onclick: () => setStatus(to), text: "Mark " + to })),
      el("button", { onclick: assignMe, text: "Assign to me" })),
    el("h3", { text: "Evidence (source events)" }),
    d.events.length === 0 ? el("p", { class: "muted", text: "No linked events (agent-health incident). See alerts below." }) :
      el("div", { class: "tw" }, el("table", null,
        el("tr", null, ["Event ID", "Time", "Kind", "Severity", "Payload"].map((h) => el("th", { text: h }))),
        d.events.map((e) => el("tr", null, el("td", null, el("code", { text: e.id.slice(0, 8) })), el("td", { text: fmt(e.occurred_at) }), el("td", { text: e.kind }), el("td", null, sevBadge(e.severity)), el("td", null, el("code", { text: short(e.payload, 140) })))))),
    el("h3", { text: "Alerts" }),
    el("ul", { class: "log" }, d.alerts.map((a) => el("li", null, sevBadge(a.severity), " ", a.rule_id, " · ", fmt(a.created_at), a.event_id ? [" · event ", el("code", { text: a.event_id.slice(0, 8) })] : " · agent heartbeat"))),
    el("h3", { text: "Response (simulation only – needs admin approval)" }),
    active ? el("div", { class: "row-actions" }, Object.entries(ACTIONS).map(([k, label]) => el("button", { onclick: () => requestApproval(k), text: "Request: " + label }))) : el("p", { class: "muted", text: "This incident is not active." }),
    d.approvals.length === 0 ? el("p", { class: "muted", text: "No approval requests yet. Nothing runs until an admin approves." }) :
      el("div", { class: "tw" }, el("table", null,
        el("tr", null, ["Action", "Requested by", "Decision", "Decided by", "Simulation", ""].map((h) => el("th", { text: h }))),
        d.approvals.map((a) => el("tr", null, el("td", { text: a.action }), el("td", { text: a.requester }), el("td", null, el("span", { class: "b st-" + pick(a.decision, ["pending", "approved", "denied"], "pending").replace("pending", "investigating").replace("approved", "active").replace("denied", "revoked"), text: a.decision })),
          el("td", { text: a.approver || "—" }), el("td", null, a.simulation_note ? el("code", { text: a.simulation_note }) : "—"),
          el("td", null, a.decision === "pending" && isAdmin() ? el("span", { class: "row-actions" }, el("button", { class: "p", onclick: () => decide(a.id, "approved"), text: "Approve" }), el("button", { class: "d", onclick: () => decide(a.id, "denied"), text: "Deny" })) : null))))),
    el("h3", { text: "Audit trail" }),
    el("ul", { class: "log" }, d.audit_trail.map((x) => el("li", null, el("code", { text: fmt(x.at) }), " ", x.actor, " · ", x.action, " · ", x.result)))
  );
}

function viewAgents() {
  return el("div", { class: "card" },
    el("div", { class: "row-actions" }, el("h2", { text: "Agents" }), isAdmin() ? el("button", { class: "p right", onclick: createToken, text: "Create enrolment token" }) : null),
    state.newToken ? el("div", { class: "note" }, "One-time token (valid 1 hour, shown once): ", el("code", { class: "token", text: state.newToken })) : null,
    el("div", { class: "tw" }, el("table", null,
      el("tr", null, ["Host", "OS", "Last heartbeat", "State", "Status", ""].map((h) => el("th", { text: h }))),
      state.agents.map((a) => el("tr", null, el("td", { text: a.hostname }), el("td", { text: a.os }), el("td", { text: ago(a.last_seen_at) }), el("td", null, stBadge(agentState(a))), el("td", null, stBadge(a.status)),
        el("td", null, a.status === "active" && isAdmin() ? el("button", { class: "d", onclick: () => revokeAgent(a), text: "Revoke" }) : null))))),
    state.agents.length === 0 ? el("p", { class: "muted", text: "No agents enrolled in this tenant." }) : null,
    el("p", { class: "muted", text: "Revoking is Admin only. A revoked agent's next request is rejected." }));
}

function viewEvents() {
  return el("div", { class: "card" }, el("h2", { text: "Event feed (live)" }),
    state.events.length === 0 ? el("p", { class: "muted", text: "No events yet." }) :
      el("div", { class: "tw" }, el("table", null,
        el("tr", null, ["Time", "Kind", "Severity", "Agent", "Payload"].map((h) => el("th", { text: h }))),
        state.events.map((e) => el("tr", null, el("td", { text: fmt(e.occurred_at) }), el("td", { text: e.kind }), el("td", null, sevBadge(e.severity)), el("td", null, el("code", { text: e.agent_id.slice(0, 8) })), el("td", null, el("code", { text: short(e.payload, 120) })))))));
}

boot();
