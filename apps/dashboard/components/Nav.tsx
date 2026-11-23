import type { ReactElement } from "react";
import { IconAlert, IconOverview, IconPulse, IconServer, ShieldMark } from "./Icons";

export type Tab = "overview" | "incidents" | "agents" | "events";

const ITEMS: { key: Tab; label: string; icon: (p: { className?: string }) => ReactElement }[] = [
  { key: "overview", label: "Overview", icon: IconOverview },
  { key: "incidents", label: "Incidents", icon: IconAlert },
  { key: "agents", label: "Agents", icon: IconServer },
  { key: "events", label: "Event feed", icon: IconPulse },
];

interface Props { tab: Tab; onTab: (t: Tab) => void; activeIncidents: number }

/** A left rail on wide screens and a bottom bar on phones. It is one element, so each button exists once. */
export default function Nav({ tab, onTab, activeIncidents }: Props) {
  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-20 flex justify-around border-t border-[#22364b] bg-[#0d1a28] pb-[env(safe-area-inset-bottom)] md:sticky md:top-0 md:h-screen md:flex-col md:justify-start md:gap-1 md:self-start md:border-r md:border-t-0 md:px-3 md:py-5 md:pb-5"
    >
      <div className="mb-6 hidden items-center gap-3 px-2 md:flex">
        <ShieldMark />
        <div className="leading-tight">
          <div className="text-sm font-semibold tracking-wide">AEGIS NEXUS AI</div>
          <div className="text-xs text-[#8fa3b8]">SOC console</div>
        </div>
      </div>
      {ITEMS.map(({ key, label, icon: Icon }) => {
        const on = tab === key;
        return (
          <button
            key={key}
            type="button"
            aria-current={on ? "page" : undefined}
            onClick={() => onTab(key)}
            className={`flex flex-1 flex-col items-center gap-1 px-2 py-2.5 text-xs transition-colors md:flex-none md:flex-row md:gap-3 md:rounded-md md:px-3 md:text-sm ${on ? "bg-[#162739] font-semibold text-[#d9ad5f]" : "text-[#8fa3b8] hover:bg-[#122234] hover:text-[#e6edf5]"}`}
          >
            <Icon />
            <span>{label}</span>
            {key === "incidents" && activeIncidents > 0 ? (
              <span aria-hidden="true" className="rounded-full bg-[#ff5d73] px-1.5 text-[11px] font-semibold text-[#1c0a0e] md:ml-auto">{activeIncidents}</span>
            ) : null}
          </button>
        );
      })}
      <p className="mt-auto hidden px-2 text-xs leading-snug text-[#5f758b] md:block">
        Lab environment. Responses are simulated and never change a real host.
      </p>
    </nav>
  );
}
