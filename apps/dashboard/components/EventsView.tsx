import { SeverityBadge } from "./Badge";
import { fmt, short } from "../lib/format";
import type { EventRow } from "../lib/types";

export default function EventsView({ events }: { events: EventRow[] }) {
  return (
    <div className="card">
      <h2 className="mb-2 text-lg font-semibold">Event feed (live)</h2>
      {events.length === 0 ? <p className="text-sm text-slate-500">No events yet.</p> : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead><tr>{["Time", "Kind", "Severity", "Agent", "Payload"].map((h) => <th key={h} className="th">{h}</th>)}</tr></thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id}>
                  <td className="td">{fmt(e.occurred_at)}</td>
                  <td className="td">{e.kind}</td>
                  <td className="td"><SeverityBadge severity={e.severity} /></td>
                  <td className="td"><span className="code">{e.agent_id.slice(0, 8)}</span></td>
                  <td className="td"><span className="code">{short(e.payload, 120)}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
