import { useState } from "react";
import { StateBadge } from "./Badge";
import { Empty, SkeletonRows } from "./States";
import { IconCheck, IconCopy } from "./Icons";
import { agentState, ago } from "../lib/format";
import type { Agent } from "../lib/types";

interface Props {
  agents: Agent[]; loaded: boolean; now: number; isAdmin: boolean; newToken: string | null;
  onCreateToken: () => void; onRevoke: (a: Agent) => void;
}

export default function AgentsView({ agents, loaded, now, isAdmin, newToken, onCreateToken, onRevoke }: Props) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    if (!newToken) return;
    try { await navigator.clipboard.writeText(newToken); setCopied(true); setTimeout(() => setCopied(false), 2000); } catch { setCopied(false); }
  };
  return (
    <section className="panel" aria-labelledby="agents-h">
      <div className="flex flex-wrap items-center gap-3 px-4 pb-3 pt-4 md:px-5">
        <h2 id="agents-h" className="h-section">Agents</h2>
        {isAdmin ? <button type="button" className="btn-primary ml-auto" onClick={onCreateToken}>Create enrolment token</button> : null}
      </div>
      {newToken ? (
        <div className="mx-4 mb-3 rounded-md border border-[#4fd1a1]/40 bg-[#4fd1a1]/10 p-3 md:mx-5">
          <p className="text-sm text-[#4fd1a1]">Enrolment token created. It is valid for 1 hour and is shown once.</p>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <span className="code select-all">{newToken}</span>
            <button type="button" className="btn" onClick={copy}>{copied ? <IconCheck className="h-4 w-4 text-[#4fd1a1]" /> : <IconCopy />}{copied ? "Copied" : "Copy token"}</button>
          </div>
        </div>
      ) : null}
      {!loaded ? <SkeletonRows rows={4} /> : agents.length === 0 ? (
        <Empty title="No agents enrolled in this tenant" action={isAdmin ? <button type="button" className="btn-primary" onClick={onCreateToken}>Create enrolment token</button> : null}>
          {isAdmin ? "Create a token, then enrol the lab agent with it." : "Ask an admin to create an enrolment token."}
        </Empty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[38rem]">
            <thead><tr>{["Host", "OS", "Last heartbeat", "State", "Credential", ""].map((h, i) => <th key={`${h}-${i}`} className="th">{h || <span className="sr-only">Actions</span>}</th>)}</tr></thead>
            <tbody>
              {agents.map((a) => (
                <tr key={a.id}>
                  <td className="td font-medium">{a.hostname}</td>
                  <td className="td text-[#8fa3b8]">{a.os}</td>
                  <td className="td text-[#8fa3b8]">{ago(a.last_seen_at, now)}</td>
                  <td className="td"><StateBadge value={agentState(a, now)} /></td>
                  <td className="td"><StateBadge value={a.status} /></td>
                  <td className="td text-right">{a.status === "active" && isAdmin ? <button type="button" className="btn-danger" onClick={() => onRevoke(a)}>Revoke</button> : null}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="px-4 py-3 text-xs text-[#8fa3b8] md:px-5">Only admins can revoke an agent. A revoked agent&apos;s next request is rejected.</p>
    </section>
  );
}
