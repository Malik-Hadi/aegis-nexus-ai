import type { LiveStatus } from "../lib/useLive";
import { TENANTS } from "../lib/types";
import type { Me } from "../lib/types";

const LABEL: Record<LiveStatus, string> = { off: "Offline", connecting: "Connecting…", live: "Live", reconnecting: "Reconnecting…" };
const COLOR: Record<LiveStatus, string> = {
  off: "bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
  connecting: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  live: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  reconnecting: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
};

export default function Header({ me, status, onLogout }: { me: Me; status: LiveStatus; onLogout: () => void }) {
  const tenant = TENANTS.find((t) => t.id === me.tenant_id)?.name ?? "tenant";
  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-slate-200 bg-white px-5 py-3 dark:border-slate-800 dark:bg-slate-900">
      <div className="font-bold tracking-wide">AEGIS NEXUS AI</div>
      <span data-testid="live-status" className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${COLOR[status]}`}>{LABEL[status]}</span>
      <div className="ml-auto text-sm text-slate-500">{me.subject} ({me.role}) · {tenant}</div>
      <button type="button" className="btn" onClick={onLogout}>Sign out</button>
    </header>
  );
}
