import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { SeverityBadge, StateBadge } from "./Badge";
import { fmt, short } from "../lib/format";
import { ACTIONS, TRANSITIONS } from "../lib/types";
import type { ApprovalAction, IncidentDetail, IncidentStatus, Me } from "../lib/types";

interface Analysis {
  mode: string;
  summary: string;
  confidence: string;
  referenced_event_ids: string[];
  proposed_next_steps: string[];
}

interface Props {
  detail: IncidentDetail; me: Me; error: string;
  onBack: () => void; onStatus: (to: IncidentStatus) => void; onAssignMe: () => void;
  onRequest: (action: ApprovalAction) => void; onDecide: (approvalId: string, decision: "approved" | "denied") => void;
}

export default function IncidentDetailView({ detail, me, error, onBack, onStatus, onAssignMe, onRequest, onDecide }: Props) {
  const inc = detail.incident;
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [analysisError, setAnalysisError] = useState("");
  const [analysing, setAnalysing] = useState(false);

  const analysisRequest = useRef<AbortController | null>(null);
  useEffect(() => () => analysisRequest.current?.abort(), []);

  async function analyse() {
    const request = new AbortController();
    analysisRequest.current = request;
    setAnalysing(true);
    setAnalysisError("");
    setAnalysis(null);
    const path = "/v1/incidents/" + encodeURIComponent(inc.id);
    try {
      const queued = await api<{ job_id: string }>("POST", path + "/analyse", undefined, request.signal);
      if (!queued.ok || !queued.data) throw new Error(queued.error || "Analysis could not be queued.");
      const resultPath = path + "/analysis/" + encodeURIComponent(queued.data.job_id);
      const deadline = Date.now() + 45000;
      while (!request.signal.aborted && Date.now() < deadline) {
        const response = await api<{ status: string; result: Analysis | null }>("GET", resultPath, undefined, request.signal);
        if (!response.ok || !response.data) throw new Error(response.error || "Could not fetch analysis.");
        if (response.data.status === "completed" && response.data.result) {
          setAnalysis(response.data.result);
          return;
        }
        if (response.data.status === "failed") throw new Error("Analysis could not complete.");
        await new Promise(resolve => setTimeout(resolve, 350));
      }
      if (!request.signal.aborted) throw new Error("Analysis is still queued. Try again shortly.");
    } catch (error) {
      if (!request.signal.aborted) setAnalysisError(error instanceof Error ? error.message : "Analysis failed.");
    } finally {
      if (!request.signal.aborted) setAnalysing(false);
    }
  }
  const isAdmin = me.role === "admin";
  const next = (TRANSITIONS[inc.status] ?? []).filter((to) => to !== "closed" || isAdmin);
  const active = inc.status === "open" || inc.status === "investigating";
  return (
    <div className="card">
      <button type="button" className="btn" onClick={onBack}>← Back</button>
      {error ? <div role="alert" className="my-3 rounded-md bg-red-100 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{error}</div> : null}
      <h2 className="mt-3 text-lg font-semibold">{inc.title}</h2>
      <p className="mt-1 flex flex-wrap items-center gap-2 text-sm">
        <SeverityBadge severity={inc.severity} /> <StateBadge value={inc.status} />
        <span>Rule <span className="code">{inc.rule_id}</span></span>
        <span>· created {fmt(inc.created_at)}</span>
        <span>· owner {inc.owner_id ? <span className="code">{inc.owner_id.slice(0, 8)}</span> : "unassigned"}</span>
      </p>
      <div className="my-3 flex flex-wrap gap-2">
        {next.map((to) => <button key={to} type="button" className="btn" onClick={() => onStatus(to)}>Mark {to}</button>)}
        <button type="button" className="btn" onClick={onAssignMe}>Assign to me</button>
      </div>

      <h3 className="mb-2 mt-5 font-semibold">Incident analysis</h3>
      <button type="button" className="btn" disabled={analysing} onClick={analyse}>
        {analysing ? "Analysing…" : "Request explanation"}
      </button>
      {analysisError && <p role="alert">{analysisError}</p>}
      {analysis && <div className="mt-2 text-sm" role="status">
        <p>{analysis.mode === "adapter" ? "Model explanation" : "Offline explanation"} · {analysis.confidence} confidence</p>
        <p>{analysis.summary}</p>
        <ul>{analysis.referenced_event_ids.map(id => <li key={id}><code>{id}</code></li>)}</ul>
        <ul>{analysis.proposed_next_steps.map(step => <li key={step}>{step}</li>)}</ul>
      </div>}

      <h3 className="mb-2 mt-5 font-semibold">Evidence (source events)</h3>
      {detail.events.length === 0 ? (
        <p className="text-sm text-slate-500">No linked events (agent-health incident). See the alerts below.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead><tr>{["Event ID", "Time", "Kind", "Severity", "Payload"].map((h) => <th key={h} className="th">{h}</th>)}</tr></thead>
            <tbody>
              {detail.events.map((e) => (
                <tr key={e.id}>
                  <td className="td"><span className="code">{e.id.slice(0, 8)}</span></td>
                  <td className="td">{fmt(e.occurred_at)}</td>
                  <td className="td">{e.kind}</td>
                  <td className="td"><SeverityBadge severity={e.severity} /></td>
                  <td className="td"><span className="code">{short(e.payload, 140)}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3 className="mb-2 mt-5 font-semibold">Alerts</h3>
      <ul className="text-sm">
        {detail.alerts.map((a) => (
          <li key={a.id} className="border-b border-slate-200 py-1.5 dark:border-slate-800">
            <SeverityBadge severity={a.severity} /> {a.rule_id} · {fmt(a.created_at)}
            {a.event_id ? <> · event <span className="code">{a.event_id.slice(0, 8)}</span></> : " · agent heartbeat"}
          </li>
        ))}
      </ul>

      <h3 className="mb-2 mt-5 font-semibold">Response (simulation only – needs admin approval)</h3>
      {active ? (
        <div className="mb-2 flex flex-wrap gap-2">
          {(Object.keys(ACTIONS) as ApprovalAction[]).map((k) => (
            <button key={k} type="button" className="btn" onClick={() => onRequest(k)}>Request: {ACTIONS[k]}</button>
          ))}
        </div>
      ) : <p className="text-sm text-slate-500">This incident is not active.</p>}
      {detail.approvals.length === 0 ? (
        <p className="text-sm text-slate-500">No approval requests yet. Nothing runs until an admin approves.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead><tr>{["Action", "Requested by", "Decision", "Decided by", "Simulation", ""].map((h) => <th key={h} className="th">{h}</th>)}</tr></thead>
            <tbody>
              {detail.approvals.map((a) => (
                <tr key={a.id}>
                  <td className="td">{a.action}</td>
                  <td className="td">{a.requester}</td>
                  <td className="td"><StateBadge value={a.decision} /></td>
                  <td className="td">{a.approver || "—"}</td>
                  <td className="td">{a.simulation_note ? <span className="code">{a.simulation_note}</span> : "—"}</td>
                  <td className="td">
                    {a.decision === "pending" && isAdmin && a.requester !== me.subject ? (
                      <span className="flex gap-2">
                        <button type="button" className="btn-primary" onClick={() => onDecide(a.id, "approved")}>Approve</button>
                        <button type="button" className="btn-danger" onClick={() => onDecide(a.id, "denied")}>Deny</button>
                      </span>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3 className="mb-2 mt-5 font-semibold">Audit trail</h3>
      <ul className="text-sm">
        {detail.audit_trail.map((x, idx) => (
          <li key={`${x.at}-${idx}`} className="border-b border-slate-200 py-1.5 dark:border-slate-800">
            <span className="code">{fmt(x.at)}</span> {x.actor} · {x.action} · {x.result}
          </li>
        ))}
      </ul>
    </div>
  );
}
