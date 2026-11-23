import { agentState, ago, clock } from "../lib/format";
import { SEV_ORDER, bucketEvents, eventsPerMinute, isActive, rank, severityCounts, sevOf } from "../lib/insights";
import type { Agent, EventRow, Incident, Severity } from "../lib/types";
import { useArrivals } from "../lib/hooks";
import { SeverityBadge } from "./Badge";
import { Empty, SkeletonRows } from "./States";

interface Props {
  incidents: Incident[]; agents: Agent[]; events: EventRow[]; now: number;
  loaded: { incidents: boolean; agents: boolean; events: boolean };
  isAdmin: boolean; onOpen: (id: string) => void; onGo: (to: "agents" | "incidents" | "events") => void;
}

function Figure({ label, value, note, tone }: { label: string; value: string; note?: string; tone?: string }) {
  return (
    <div className="px-4 py-3 md:px-5">
      <div className="text-xs text-[#8fa3b8]">{label}</div>
      <div className={`mt-0.5 text-3xl font-semibold leading-none ${tone ?? ""}`}>{value}</div>
      {note ? <div className="mt-1.5 text-xs text-[#8fa3b8]">{note}</div> : null}
    </div>
  );
}

/** The telemetry timeline. Bars are events per 2 minutes, coloured by the worst severity in that slot. Diamonds mark incidents. */
function Timeline({ events, incidents, now }: { events: EventRow[]; incidents: Incident[]; now: number }) {
  const W = 720, H = 190, padL = 30, padR = 8, padT = 22, padB = 24;
  const buckets = bucketEvents(events, now, 60, 2);
  const max = Math.max(4, ...buckets.map((b) => b.count));
  const plotW = W - padL - padR, plotH = H - padT - padB;
  const slot = plotW / buckets.length;
  const barW = slot - 4;
  const from = buckets[0].start;
  const span = now - from;
  const total = buckets.reduce((n, b) => n + b.count, 0);
  const marks = incidents
    .map((i) => ({ i, t: new Date(i.created_at).getTime() }))
    .filter((m) => Number.isFinite(m.t) && m.t >= from && m.t <= now);
  const ticks = [0, Math.round(max / 2), max];
  return (
    <div>
      <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="img" aria-label={`Events in the last 60 minutes: ${total} in the loaded list, ${marks.length} incidents opened.`}>
        {ticks.map((t) => {
          const y = padT + plotH - (t / max) * plotH;
          return (
            <g key={t}>
              <line x1={padL} x2={W - padR} y1={y} y2={y} stroke="#22364b" strokeWidth="1" strokeDasharray={t === 0 ? undefined : "3 5"} />
              <text x={padL - 6} y={y + 4} textAnchor="end" className="fill-[#8fa3b8]" fontSize="11">{t}</text>
            </g>
          );
        })}
        {buckets.map((b, i) => {
          if (b.count === 0) return null;
          const h = Math.max(3, (b.count / max) * plotH);
          return (
            <rect key={b.start} x={padL + i * slot + 2} y={padT + plotH - h} width={barW} height={h} rx="2" className={`fill-${b.top}`} opacity="0.92">
              <title>{`${clock(new Date(b.start).toISOString())} to ${clock(new Date(b.end).toISOString())}: ${b.count} events, highest severity ${b.top}`}</title>
            </rect>
          );
        })}
        {marks.map(({ i, t }) => {
          const x = padL + ((t - from) / span) * plotW;
          return (
            <g key={i.id}>
              <line x1={x} x2={x} y1={padT - 6} y2={padT + plotH} stroke="#d9ad5f" strokeWidth="1" strokeDasharray="2 4" opacity="0.55" />
              <path d={`M${x} ${padT - 14} l6 7 l-6 7 l-6 -7 Z`} className={`fill-${sevOf(i.severity)}`}>
                <title>{`${i.title} (${i.severity}) at ${clock(i.created_at)}`}</title>
              </path>
            </g>
          );
        })}
        <text x={padL} y={H - 6} className="fill-[#8fa3b8]" fontSize="11">60 min ago</text>
        <text x={padL + plotW / 2} y={H - 6} textAnchor="middle" className="fill-[#8fa3b8]" fontSize="11">30 min ago</text>
        <text x={W - padR} y={H - 6} textAnchor="end" className="fill-[#8fa3b8]" fontSize="11">now</text>
      </svg>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[#8fa3b8]">
        {(["critical", "high", "medium", "low", "info"] as Severity[]).map((s) => (
          <span key={s} className="inline-flex items-center gap-1.5"><span className={`dot sev-${s}`} aria-hidden="true" />{s}</span>
        ))}
        <span className="inline-flex items-center gap-1.5"><svg viewBox="0 0 12 14" className="h-3 w-3 fill-[#d9ad5f]" aria-hidden="true"><path d="M6 0l6 7-6 7-6-7Z" /></svg>incident opened</span>
      </div>
    </div>
  );
}

function SeveritySplit({ incidents }: { incidents: Incident[] }) {
  const active = incidents.filter(isActive);
  const counts = severityCounts(active);
  const total = active.length;
  let x = 0;
  return (
    <div>
      <svg width="100%" height="10" className="block overflow-hidden rounded-full" role="img" aria-label={`Active incidents by severity: ${SEV_ORDER.map((s) => `${counts[s]} ${s}`).join(", ")}`}>
        <rect x="0" y="0" width="100%" height="10" fill="#162739" />
        {total > 0 ? SEV_ORDER.map((s) => {
          if (counts[s] === 0) return null;
          const w = (counts[s] / total) * 100;
          const el = <rect key={s} x={`${x}%`} y="0" width={`${w}%`} height="10" className={`fill-${s}`} />;
          x += w;
          return el;
        }) : null}
      </svg>
      <ul className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-5 lg:grid-cols-2 xl:grid-cols-5">
        {SEV_ORDER.map((s) => (
          <li key={s} className="flex items-center gap-2"><span className={`dot sev-${s}`} aria-hidden="true" /><span className="text-[#8fa3b8]">{s}</span><span className="ml-auto font-medium">{counts[s]}</span></li>
        ))}
      </ul>
    </div>
  );
}

export default function Overview({ incidents, agents, events, now, loaded, isAdmin, onOpen, onGo }: Props) {
  const active = incidents.filter(isActive);
  const urgent = active.filter((i) => rank(sevOf(i.severity)) >= rank("high"));
  const states = agents.map((a) => agentState(a, now));
  const online = states.filter((s) => s === "online").length;
  const attention = states.filter((s) => s === "late" || s === "offline").length;
  const epm = eventsPerMinute(events, now, 5, 100);
  const everyoneLoaded = loaded.incidents && loaded.agents && loaded.events;
  const freshIncidents = useArrivals(incidents.map((i) => i.id), loaded.incidents);
  const freshEvents = useArrivals(events.map((e) => e.id), loaded.events);
  const latestIncidents = [...active].sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()).slice(0, 6);
  const latestEvents = events.slice(0, 8);
  const nothingYet = everyoneLoaded && agents.length === 0 && events.length === 0 && incidents.length === 0;

  return (
    <div className="space-y-4 md:space-y-5">
      <section aria-label="Status" className="panel grid grid-cols-2 divide-y divide-[#22364b] md:grid-cols-4 md:divide-x md:divide-y-0">
        <Figure label="Active incidents" value={loaded.incidents ? String(active.length) : "–"} note={urgent.length > 0 ? `${urgent.length} high or critical` : "none high or critical"} tone={urgent.length > 0 ? "text-[#ff5d73]" : ""} />
        <Figure label="Agents online" value={loaded.agents ? `${online}/${agents.length}` : "–"} note={attention > 0 ? `${attention} late or offline` : "all reporting"} tone={attention > 0 ? "text-[#f2c14e]" : ""} />
        <Figure label="Events per minute" value={loaded.events ? `${epm.capped ? "≥" : ""}${epm.value}` : "–"} note="average over 5 minutes" />
        <Figure label="Events loaded" value={loaded.events ? String(events.length) : "–"} note="latest 100 are kept" />
      </section>

      {nothingYet ? (
        <section className="panel">
          <Empty
            title="Nothing has reported yet"
            action={isAdmin ? <button type="button" className="btn-primary" onClick={() => onGo("agents")}>Create an enrolment token</button> : null}
          >
            Enrol an agent and its heartbeats, events and incidents will appear here as they arrive.
            {isAdmin ? "" : " Ask an admin to create an enrolment token."}
          </Empty>
        </section>
      ) : null}

      <section className="panel panel-pad" aria-labelledby="tl-h">
        <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="tl-h" className="h-section">Telemetry, last 60 minutes</h2>
          <button type="button" className="text-sm text-[#d9ad5f] underline-offset-4 hover:underline" onClick={() => onGo("events")}>Open event feed</button>
        </div>
        {loaded.events ? <Timeline events={events} incidents={incidents} now={now} /> : <SkeletonRows rows={3} />}
        <p className="mt-2 text-xs text-[#5f758b]">Built from the latest {events.length} events the console has loaded.</p>
      </section>

      <div className="grid gap-4 md:gap-5 lg:grid-cols-5">
        <section className="panel lg:col-span-3" aria-labelledby="inc-h">
          <div className="flex items-center justify-between px-4 pb-1 pt-4 md:px-5">
            <h2 id="inc-h" className="h-section">Active incidents</h2>
            <button type="button" className="text-sm text-[#d9ad5f] underline-offset-4 hover:underline" onClick={() => onGo("incidents")}>All incidents</button>
          </div>
          {!loaded.incidents ? <SkeletonRows /> : latestIncidents.length === 0 ? (
            <Empty title="No active incidents">A new incident appears here the moment a detection rule fires.</Empty>
          ) : (
            <ul>
              {latestIncidents.map((i) => (
                <li key={i.id} className={`border-t border-[#1a2c3f] ${freshIncidents.has(i.id) ? "arrive" : ""}`}>
                  <button type="button" onClick={() => onOpen(i.id)} className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-[#13243a] md:px-5">
                    <SeverityBadge severity={i.severity} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{i.title}</span>
                      <span className="block text-xs text-[#8fa3b8]">{i.rule_id} · {i.event_count} {i.event_count === 1 ? "event" : "events"} · {ago(i.created_at, now)}</span>
                    </span>
                    <span className="text-xs text-[#8fa3b8]">{i.status}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <div className="space-y-4 md:space-y-5 lg:col-span-2">
          <section className="panel panel-pad" aria-labelledby="sev-h">
            <h2 id="sev-h" className="h-section mb-3">Active incidents by severity</h2>
            <SeveritySplit incidents={incidents} />
          </section>

          <section className="panel" aria-labelledby="ag-h">
            <h2 id="ag-h" className="h-section px-4 pb-1 pt-4 md:px-5">Agent health</h2>
            {!loaded.agents ? <SkeletonRows rows={2} /> : agents.length === 0 ? (
              <Empty title="No agents enrolled">Agents appear here once they enrol.</Empty>
            ) : (
              <ul>
                {agents.slice(0, 6).map((a) => {
                  const s = agentState(a, now);
                  const tone = s === "online" ? "st-good" : s === "late" ? "st-warn" : s === "never" ? "st-neutral" : "st-bad";
                  return (
                    <li key={a.id} className="flex items-center gap-3 border-t border-[#1a2c3f] px-4 py-2.5 md:px-5">
                      <span className={`dot ${tone} border-0`} aria-hidden="true" />
                      <span className="min-w-0 flex-1 truncate text-sm">{a.hostname}</span>
                      <span className="text-xs text-[#8fa3b8]">{s} · {ago(a.last_seen_at, now)}</span>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        </div>
      </div>

      <section className="panel" aria-labelledby="feed-h">
        <h2 id="feed-h" className="h-section px-4 pb-1 pt-4 md:px-5">Latest events</h2>
        {!loaded.events ? <SkeletonRows rows={3} /> : latestEvents.length === 0 ? (
          <Empty title="No events yet">Events from enrolled agents stream in here without a refresh.</Empty>
        ) : (
          <ul>
            {latestEvents.map((e) => (
              <li key={e.id} className={`flex items-center gap-3 border-t border-[#1a2c3f] px-4 py-2 text-sm md:px-5 ${freshEvents.has(e.id) ? "arrive" : ""}`}>
                <span className="w-16 shrink-0 font-mono text-xs text-[#8fa3b8]">{clock(e.occurred_at)}</span>
                <SeverityBadge severity={e.severity} />
                <span className="min-w-0 flex-1 truncate">{e.kind}</span>
                <span className="code hidden sm:inline">{e.agent_id.slice(0, 8)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
