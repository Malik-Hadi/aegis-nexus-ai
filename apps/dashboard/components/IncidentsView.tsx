import { SeverityBadge, StateBadge } from "./Badge";
import { Empty, SkeletonRows } from "./States";
import { ago, fmt } from "../lib/format";
import { useArrivals } from "../lib/hooks";
import { STATUSES } from "../lib/types";
import type { Incident, IncidentStatus } from "../lib/types";

interface Props {
  incidents: Incident[]; loaded: boolean; now: number;
  filter: IncidentStatus | ""; onFilter: (s: IncidentStatus | "") => void; onOpen: (id: string) => void;
}

const PILLS: { value: IncidentStatus | ""; label: string }[] = [{ value: "", label: "All" }, ...STATUSES.map((s) => ({ value: s, label: s[0].toUpperCase() + s.slice(1) }))];

export default function IncidentsView({ incidents, loaded, now, filter, onFilter, onOpen }: Props) {
  const fresh = useArrivals(incidents.map((i) => i.id), loaded);
  return (
    <section className="panel" aria-labelledby="incidents-h">
      <div className="flex flex-wrap items-center gap-3 px-4 pb-3 pt-4 md:px-5">
        <h2 id="incidents-h" className="h-section">Incidents</h2>
        <div role="group" aria-label="Filter by status" className="ml-auto flex flex-wrap gap-1.5">
          {PILLS.map((p) => (
            <button
              key={p.value || "all"}
              type="button"
              aria-pressed={filter === p.value}
              onClick={() => onFilter(p.value)}
              className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${filter === p.value ? "border-[#d9ad5f] bg-[#d9ad5f]/15 text-[#d9ad5f]" : "border-[#2c4560] text-[#8fa3b8] hover:text-[#e6edf5]"}`}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>
      {!loaded ? <SkeletonRows rows={5} /> : incidents.length === 0 ? (
        <Empty title={filter ? `No ${filter} incidents` : "No incidents yet"}>
          {filter ? "Choose All to see every incident." : "An incident is created when a detection rule matches events from an enrolled agent. It will show up here live."}
        </Empty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[40rem]">
            <thead>
              <tr>{["Opened", "Incident", "Rule", "Severity", "Status", "Events"].map((h) => <th key={h} className="th">{h}</th>)}</tr>
            </thead>
            <tbody>
              {incidents.map((i) => (
                <tr key={i.id} className={`cursor-pointer transition-colors hover:bg-[#13243a] ${fresh.has(i.id) ? "arrive" : ""}`} onClick={() => onOpen(i.id)}>
                  <td className="td whitespace-nowrap text-[#8fa3b8]" title={fmt(i.created_at)}>{ago(i.created_at, now)}</td>
                  <td className="td"><button type="button" className="text-left font-medium hover:text-[#d9ad5f]" onClick={(e) => { e.stopPropagation(); onOpen(i.id); }}>{i.title}</button></td>
                  <td className="td"><span className="code">{i.rule_id}</span></td>
                  <td className="td"><SeverityBadge severity={i.severity} /></td>
                  <td className="td"><StateBadge value={i.status} /></td>
                  <td className="td">{i.event_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
