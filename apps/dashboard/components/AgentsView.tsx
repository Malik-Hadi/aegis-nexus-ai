import { StateBadge } from "./Badge";
import { agentState, ago } from "../lib/format";
import type { Agent } from "../lib/types";

interface Props {
  agents: Agent[]; isAdmin: boolean; newToken: string | null;
  onCreateToken: () => void; onRevoke: (a: Agent) => void;
}

export default function AgentsView({ agents, isAdmin, newToken, onCreateToken, onRevoke }: Props) {
  return (
    <div className="card">
      <div className="mb-2 flex items-center gap-3">
        <h2 className="text-lg font-semibold">Agents</h2>
        {isAdmin ? <button type="button" className="btn-primary ml-auto" onClick={onCreateToken}>Create enrolment token</button> : null}
      </div>
      {newToken ? (
        <div className="my-2 rounded-md bg-green-100 px-3 py-2 text-sm text-green-800 dark:bg-green-950 dark:text-green-300">
          One-time token (valid 1 hour, shown once): <span className="code">{newToken}</span>
        </div>
      ) : null}
      {agents.length === 0 ? <p className="text-sm text-slate-500">No agents enrolled in this tenant.</p> : (
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead><tr>{["Host", "OS", "Last heartbeat", "State", "Status", ""].map((h) => <th key={h} className="th">{h}</th>)}</tr></thead>
            <tbody>
              {agents.map((a) => (
                <tr key={a.id}>
                  <td className="td">{a.hostname}</td>
                  <td className="td">{a.os}</td>
                  <td className="td">{ago(a.last_seen_at)}</td>
                  <td className="td"><StateBadge value={agentState(a)} /></td>
                  <td className="td"><StateBadge value={a.status} /></td>
                  <td className="td">{a.status === "active" && isAdmin ? <button type="button" className="btn-danger" onClick={() => onRevoke(a)}>Revoke</button> : null}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-3 text-sm text-slate-500">Revoking is Admin only. A revoked agent&apos;s next request is rejected.</p>
    </div>
  );
}
