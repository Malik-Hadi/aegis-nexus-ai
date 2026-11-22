import { useEffect, useRef, useState } from "react";
import type { LiveMessage } from "./types";

export type LiveStatus = "off" | "connecting" | "live" | "reconnecting";

export interface LiveHandlers {
  onMessage: (m: LiveMessage) => void;
  onResync: () => void;   // the server dropped messages for a slow browser: reload the lists
  onCatchUp: () => void;  // the connection came back after a drop: reload the lists
  onExpired: () => void;  // the login expired
}

/** Real-time push from the API (Server-Sent Events). The browser reconnects by itself; we also handle a closed stream. */
export function useLive(enabled: boolean, handlers: LiveHandlers): LiveStatus {
  const [status, setStatus] = useState<LiveStatus>("off");
  const latest = useRef<LiveHandlers>(handlers);
  latest.current = handlers;

  useEffect(() => {
    if (!enabled) { setStatus("off"); return; }
    let es: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let wasDown = false;
    let stopped = false;

    const connect = (): void => {
      if (stopped) return;
      setStatus("connecting");
      const source = new EventSource("/v1/stream");
      es = source;
      source.onopen = () => {
        setStatus("live");
        if (wasDown) { wasDown = false; latest.current.onCatchUp(); }
      };
      source.onmessage = (e: MessageEvent<string>) => {
        try { latest.current.onMessage(JSON.parse(e.data) as LiveMessage); } catch { /* ignore malformed */ }
      };
      source.addEventListener("resync", () => latest.current.onResync());
      source.addEventListener("expired", () => latest.current.onExpired());
      source.onerror = () => {
        wasDown = true;
        setStatus("reconnecting");
        if (source.readyState === EventSource.CLOSED) {
          // The browser gave up (for example the server answered 401). Find out why, then retry.
          source.close();
          es = null;
          fetch("/v1/me", { credentials: "same-origin" })
            .then((r) => { if (r.status === 401) latest.current.onExpired(); else timer = setTimeout(connect, 3000); })
            .catch(() => { timer = setTimeout(connect, 3000); });
        }
      };
    };

    connect();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      if (es) es.close();
    };
  }, [enabled]);

  return status;
}
