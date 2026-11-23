import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { SeverityBadge, StateBadge } from "./Badge";
import { ErrorNote } from "./States";
import { IconBack } from "./Icons";
import { fmt, short } from "../lib/format";
import { sevOf } from "../lib/insights";
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

const BANNER: Record<string, string> = {
  critical: "border-l-[#ff5d73]", high: "border-l-[#ff8a4c]", medium: "border-l-[#f2c14e]", low: "border-l-[#5aa9ff]", info: "border-l-[#8fa3b8]",
};

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
  const events = [...detail.events].sort((a, b) => new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime());

  return (
    <div className="space-y-4 md:space-y-5">
      <button type="button" className="btn" onClick={onBack}><IconBack />Back to incidents</button>
      {error ? <ErrorNote>{error}</ErrorNote> : null}

      <section className={`panel panel-pad border-l-4 ${BANNER[sevOf(inc.severity)]}`}>
        <div className="flex flex-wrap items-center gap-2">
          <SeverityBadge severity={inc.severity} /><StateBadge value={inc.status} />
        </div>
        <h2 className="mt-2 text-xl font-semibold md:text-2xl">{inc.title}</h2>
        <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-2 text-sm md:grid-cols-4">
          <div><dt className="text-xs text-[#8fa3b8]">Rule</dt><dd><span className="code">{inc.rule_id}</span></dd></div>
          <div><dt className="text-xs text-[#8fa3b8]">Opened</dt><dd>{fmt(inc.created_at)}</dd></div>
          <div><dt className="text-xs text-[#8fa3b8]">Evidence</dt><dd>{detail.events.length} {detail.events.length === 1 ? "event" : "events"}</dd></div>
          <div><dt className="text-xs text-[#8fa3b8]">Owner</dt><dd>{inc.owner_id ? <span className="code">{inc.owner_id.slice(0, 8)}</span> : "Unassigned"}</dd></div>
        </dl>
        <div className="mt-4 flex flex-wrap gap-2">
          {next.map((to) => <button key={to} type="button" className="btn" onClick={() => onStatus(to)}>Mark {to}</button>)}
          <button type="button" className="btn" onClick={onAssignMe}>Assign to me</button>
        </div>
      </section>

      <div className="grid gap-4 md:gap-5 lg:grid-cols-5">
        <div className="space-y-4 md:space-y-5 lg:col-span-3">
          <section className="panel panel-pad" aria-labelledby="ev-h">
            <h3 id="ev-h" className="h-section">Evidence timeline</h3>
            <p className="mb-4 text-xs text-[#8fa3b8]">The source events behind this incident, oldest first. Event text is untrusted and is shown as plain text.</p>
            {events.length === 0 ? (
              <p className="text-sm text-[#8fa3b8]">No linked events. This is an agent-health incident, so see the alerts below.</p>
            ) : (
              <ol className="relative ml-2 border-l border-[#2c4560]">
                {events.map((e) => (
                  <li key={e.id} className="relative pb-5 pl-6 last:pb-0">
                    <span className={`dot sev-${sevOf(e.severity)} absolute -left-[0.34rem] top-1.5 ring-4 ring-[#101d2c]`} aria-hidden="true" />
                    <div className="flex flex-wrap items-center gap-2 text-sm">
                      <span className="font-mono text-xs text-[#8fa3b8]">{fmt(e.occurred_at)}</span>
                      <SeverityBadge severity={e.severity} />
                      <span className="font-medium">{e.kind}</span>
                      <span className="code">{e.id.slice(0, 8)}</span>
                    </div>
                    <p className="mt-1.5"><span className="code">{short(e.payload, 160)}</span></p>
                  </li>
                ))}
              </ol>
            )}
          </section>

          <section className="panel panel-pad" aria-labelledby="al-h">
            <h3 id="al-h" className="h-section mb-2">Rules that fired</h3>
            <ul className="text-sm">
              {detail.alerts.map((a) => (
                <li key={a.id} className="flex flex-wrap items-center gap-2 border-t border-[#1a2c3f] py-2 first:border-t-0">
                  <SeverityBadge severity={a.severity} />
                  <span className="code">{a.rule_id}</span>
                  <span className="text-[#8fa3b8]">{fmt(a.created_at)}</span>
                  <span className="text-[#8fa3b8]">{a.event_id ? <>event <span className="code">{a.event_id.slice(0, 8)}</span></> : "agent heartbeat"}</span>
                </li>
              ))}
            </ul>
          </section>
        </div>

        <div className="space-y-4 md:space-y-5 lg:col-span-2">
          <section className="panel panel-pad" aria-labelledby="an-h">
            <h3 id="an-h" className="h-section">Incident analysis</h3>
            <p className="mb-3 text-xs text-[#8fa3b8]">A summary written only from this incident&apos;s evidence. It cannot change anything.</p>
            <button type="button" className="btn-primary" disabled={analysing} onClick={analyse}>
              {analysing ? "Analysing…" : "Request explanation"}
            </button>
            {analysisError && <p role="alert" className="mt-3 text-sm text-[#ff9aa8]">{analysisError}</p>}
            {analysis && (
              <div className="mt-4 space-y-3 text-sm" role="status">
                <div className="flex flex-wrap gap-2">
                  <span className="chip st-neutral">{analysis.mode === "adapter" ? "Model explanation" : "Offline explanation"}</span>
                  <span className="chip st-neutral">{analysis.confidence} confidence</span>
                </div>
                <p className="leading-relaxed">{analysis.summary}</p>
                {analysis.referenced_event_ids.length > 0 ? (
                  <div><div className="mb-1 text-xs text-[#8fa3b8]">Cited events</div><ul className="flex flex-wrap gap-1.5">{analysis.referenced_event_ids.map((id) => <li key={id}><code className="code">{id}</code></li>)}</ul></div>
                ) : null}
                {analysis.proposed_next_steps.length > 0 ? (
                  <div><div className="mb-1 text-xs text-[#8fa3b8]">Suggested next steps</div><ul className="list-disc space-y-1 pl-5">{analysis.proposed_next_steps.map((step) => <li key={step}>{step}</li>)}</ul></div>
                ) : null}
              </div>
            )}
          </section>

          <section className="panel panel-pad" aria-labelledby="rs-h">
            <h3 id="rs-h" className="h-section">Response</h3>
            <p className="mb-3 text-xs text-[#8fa3b8]">Simulation only. Nothing runs until an admin who did not make the request approves it.</p>
            {active ? (
              <div className="mb-3 flex flex-wrap gap-2">
                {(Object.keys(ACTIONS) as ApprovalAction[]).map((k) => (
                  <button key={k} type="button" className="btn" onClick={() => onRequest(k)}>Request: {ACTIONS[k]}</button>
                ))}
              </div>
            ) : <p className="mb-3 text-sm text-[#8fa3b8]">This incident is not active, so new responses cannot be requested.</p>}
            {detail.approvals.length === 0 ? (
              <p className="text-sm text-[#8fa3b8]">No approval requests yet.</p>
            ) : (
              <ul className="space-y-3">
                {detail.approvals.map((a) => (
                  <li key={a.id} className="rounded-md border border-[#22364b] bg-[#0d1a28] p-3 text-sm">
                    <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{a.action}</span><StateBadge value={a.decision} /></div>
                    <div className="mt-1 text-xs text-[#8fa3b8]">Requested by {a.requester}{a.approver ? ` · decided by ${a.approver}` : ""}</div>
                    {a.simulation_note ? <p className="mt-2"><span className="code">{a.simulation_note}</span></p> : null}
                    {a.decision === "pending" && isAdmin && a.requester !== me.subject ? (
                      <div className="mt-3 flex gap-2">
                        <button type="button" className="btn-primary" onClick={() => onDecide(a.id, "approved")}>Approve</button>
                        <button type="button" className="btn-danger" onClick={() => onDecide(a.id, "denied")}>Deny</button>
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="panel panel-pad" aria-labelledby="au-h">
            <h3 id="au-h" className="h-section mb-2">Audit trail</h3>
            <ul className="text-sm">
              {detail.audit_trail.map((x, idx) => (
                <li key={`${x.at}-${idx}`} className="border-t border-[#1a2c3f] py-2 first:border-t-0">
                  <div className="font-mono text-xs text-[#8fa3b8]">{fmt(x.at)}</div>
                  <div>{x.actor} · {x.action} · <span className={x.result === "success" || x.result === "ok" ? "text-[#4fd1a1]" : ""}>{x.result}</span></div>
                </li>
              ))}
            </ul>
          </section>
        </div>
      </div>
    </div>
  );
}
