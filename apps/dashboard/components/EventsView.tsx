import { useState } from "react";
import { SeverityBadge } from "./Badge";
import { Empty, SkeletonRows } from "./States";
import { fmt, short } from "../lib/format";
import { SEV_ORDER, sevOf } from "../lib/insights";
import { useArrivals } from "../lib/hooks";
import type { EventRow, Severity } from "../lib/types";

export default function EventsView({ events, loaded }: { events: EventRow[]; loaded: boolean }) {
  const [sev, setSev] = useState<Severity | "">("");
  const [q, setQ] = useState("");
  const fresh = useArrivals(events.map((e) => e.id), loaded);
  const needle = q.trim().toLowerCase();
  const shown = events.filter((e) => (sev === "" || sevOf(e.severity) === sev) && (needle === "" || e.kind.toLowerCase().includes(needle)));
  return (
    <section className="panel" aria-labelledby="events-h">
      <div className="flex flex-wrap items-center gap-3 px-4 pb-3 pt-4 md:px-5">
        <h2 id="events-h" className="h-section">Event feed</h2>
        <span className="text-xs text-[#8fa3b8]">updates without a refresh</span>
        <input aria-label="Filter by event kind" placeholder="Filter by kind" value={q} onChange={(e) => setQ(e.target.value)} className="input ml-auto max-w-[14rem]" />
      </div>
      <div role="group" aria-label="Filter by severity" className="flex flex-wrap gap-1.5 px-4 pb-3 md:px-5">
        {(["", ...SEV_ORDER] as (Severity | "")[]).map((s) => (
          <button key={s || "all"} type="button" aria-pressed={sev === s} onClick={() => setSev(s)}
            className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${sev === s ? "border-[#d9ad5f] bg-[#d9ad5f]/15 text-[#d9ad5f]" : "border-[#2c4560] text-[#8fa3b8] hover:text-[#e6edf5]"}`}>
            {s === "" ? "All severities" : s}
          </button>
        ))}
      </div>
      {!loaded ? <SkeletonRows rows={6} /> : events.length === 0 ? (
        <Empty title="No events yet">Events from enrolled agents appear here as they arrive.</Empty>
      ) : shown.length === 0 ? (
        <Empty title="No events match these filters">Clear the severity or kind filter to see all {events.length} loaded events.</Empty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[40rem]">
            <thead><tr>{["Time", "Kind", "Severity", "Agent", "Payload"].map((h) => <th key={h} className="th">{h}</th>)}</tr></thead>
            <tbody>
              {shown.map((e) => (
                <tr key={e.id} className={fresh.has(e.id) ? "arrive" : ""}>
                  <td className="td whitespace-nowrap text-[#8fa3b8]">{fmt(e.occurred_at)}</td>
                  <td className="td whitespace-nowrap"><span>{e.kind}</span></td>
                  <td className="td"><SeverityBadge severity={e.severity} /></td>
                  <td className="td"><span className="code">{e.agent_id.slice(0, 8)}</span></td>
                  <td className="td"><span className="code">{short(e.payload, 120)}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
