import type { LiveStatus } from "../lib/useLive";
import { TENANTS } from "../lib/types";
import type { Me } from "../lib/types";
import { IconBell, IconBellOff, IconLogout, ShieldMark } from "./Icons";

const LABEL: Record<LiveStatus, string> = { off: "Offline", connecting: "Connecting…", live: "Live", reconnecting: "Reconnecting…" };
const TONE: Record<LiveStatus, string> = { off: "st-neutral", connecting: "st-warn", live: "st-good", reconnecting: "st-warn" };

interface Props {
  me: Me; status: LiveStatus; title: string; soundOn: boolean;
  onSound: () => void; onLogout: () => void;
}

export default function Header({ me, status, title, soundOn, onSound, onLogout }: Props) {
  const tenant = TENANTS.find((t) => t.id === me.tenant_id)?.name ?? "tenant";
  return (
    <header className="sticky top-0 z-10 flex items-center gap-3 border-b border-[#22364b] bg-[#0a1420]/95 px-4 py-3 backdrop-blur md:px-6">
      <div className="flex items-center gap-2 md:hidden"><ShieldMark className="h-6 w-6" /><span className="text-sm font-semibold">AEGIS NEXUS AI</span></div>
      <h1 className="hidden text-lg font-semibold md:block">{title}</h1>
      <span data-testid="live-status" className={`chip gap-1.5 ${TONE[status]}`}>
        <span className={`dot ${status === "live" ? "live-dot" : ""}`} aria-hidden="true" />{LABEL[status]}
      </span>
      <div className="ml-auto flex items-center gap-2 md:gap-3">
        <div className="hidden text-right leading-tight sm:block">
          <div className="text-sm font-medium">{me.subject}</div>
          <div className="text-xs text-[#8fa3b8]">{me.role} · {tenant}</div>
        </div>
        <button type="button" className="btn px-2" onClick={onSound} aria-pressed={soundOn} aria-label={soundOn ? "Turn alert sound off" : "Turn alert sound on"} title={soundOn ? "Alert sound on" : "Alert sound off"}>
          {soundOn ? <IconBell className="h-4 w-4 text-[#d9ad5f]" /> : <IconBellOff className="h-4 w-4" />}
        </button>
        <button type="button" className="btn" onClick={onLogout}><IconLogout className="h-4 w-4" /><span className="hidden sm:inline">Sign out</span><span className="sm:hidden sr-only">Sign out</span></button>
      </div>
    </header>
  );
}
