import type { ChangeEvent } from "react";
import { SeverityBadge, StateBadge } from "./Badge";
import { agentState, fmt } from "../lib/format";
import { STATUSES } from "../lib/types";
import type { Agent, EventRow, Incident, IncidentStatus } from "../lib/types";

interface Props {
  incidents: Incident[]; agents: Agent[]; events: EventRow[];
  filter: IncidentStatus | ""; onFilter: (s: IncidentStatus | "") => void; onOpen: (id: string) => void;
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
      <b className="block text-2xl">{value}</b>
      <span className="text-sm text-slate-500">{label}</span>
    </div>
  );
}

export default function IncidentsView({ incidents, agents, events, filter, onFilter, onOpen }: Props) {
  const active = incidents.filter((i) => i.status === "open" || i.status === "investigating");
  const high = active.filter((i) => i.severity === "high" || i.severity === "critical");
  const online = agents.filter((a) => agentState(a) === "online").length;
  const change = (e: ChangeEvent<HTMLSelectElement>) => onFilter(e.target.value as IncidentStatus | "");
  return (
    <div>
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat value={active.length} label="Active incidents" />
        <Stat value={high.length} label="High severity active" />
        <Stat value={online} label="Agents online" />
        <Stat value={events.length} label="Recent events" />
      </div>
      <div className="card">
        <div className="mb-2 flex items-center gap-3">
          <h2 className="text-lg font-semibold">Incidents</h2>
          <select aria-label="Filter by status" className="input ml-auto w-auto" value={filter} onChange={change}>
            <option value="">All statuses</option>
            {STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
        </div>
        {incidents.length === 0 ? (
          <p className="text-sm text-slate-500">No incidents yet. They appear here live when a rule fires.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr>{["Created", "Title", "Rule", "Severity", "Status", "Evidence"].map((h) => <th key={h} className="th">{h}</th>)}</tr>
              </thead>
              <tbody>
                {incidents.map((i) => (
                  <tr key={i.id} className="cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800" onClick={() => onOpen(i.id)}>
                    <td className="td">{fmt(i.created_at)}</td>
                    <td className="td">{i.title}</td>
                    <td className="td">{i.rule_id}</td>
                    <td className="td"><SeverityBadge severity={i.severity} /></td>
                    <td className="td"><StateBadge value={i.status} /></td>
                    <td className="td">{i.event_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
