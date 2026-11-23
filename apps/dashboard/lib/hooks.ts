import { useEffect, useRef, useState } from "react";

/** A clock that re-renders the page every `ms`, so "online/late/offline" and the timeline stay current without new data. */
export function useNow(ms = 15_000): number {
  const [now, setNow] = useState<number>(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

/** Returns the ids that appeared since the last list, for about 2.6 s, so new rows can glow once. The first load never glows. */
export function useArrivals(ids: string[], ready: boolean): Set<string> {
  const seen = useRef<Set<string> | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [fresh, setFresh] = useState<Set<string>>(() => new Set());
  const key = ids.join("|");
  useEffect(() => {
    if (!ready) return;
    if (seen.current === null) { seen.current = new Set(ids); return; }
    const known = seen.current;
    const added = ids.filter((id) => !known.has(id));
    if (added.length === 0) return;
    for (const id of added) known.add(id);
    setFresh(new Set(added));
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setFresh(new Set()), 2600);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, ready]);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  return fresh;
}

type AudioCtor = typeof AudioContext;
let ctx: AudioContext | null = null;

/** Call from a click so the browser allows sound. */
export function armSound(): void {
  try {
    const AC: AudioCtor | undefined = window.AudioContext ?? (window as unknown as { webkitAudioContext?: AudioCtor }).webkitAudioContext;
    if (!AC) return;
    if (!ctx) ctx = new AC();
    void ctx.resume();
  } catch { /* sound is optional */ }
}

/** Two short tones for a new high or critical incident. Does nothing until armSound ran. */
export function playAlert(): void {
  try {
    if (!ctx) return;
    const t0 = ctx.currentTime;
    [660, 880].forEach((freq, i) => {
      const osc = ctx!.createOscillator();
      const gain = ctx!.createGain();
      osc.type = "sine"; osc.frequency.value = freq;
      gain.gain.setValueAtTime(0.0001, t0 + i * 0.18);
      gain.gain.exponentialRampToValueAtTime(0.12, t0 + i * 0.18 + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, t0 + i * 0.18 + 0.16);
      osc.connect(gain); gain.connect(ctx!.destination);
      osc.start(t0 + i * 0.18); osc.stop(t0 + i * 0.18 + 0.18);
    });
  } catch { /* ignore */ }
}

export function readSoundPref(): boolean {
  try { return window.localStorage.getItem("aegis.sound") === "on"; } catch { return false; }
}
export function saveSoundPref(on: boolean): void {
  try { window.localStorage.setItem("aegis.sound", on ? "on" : "off"); } catch { /* ignore */ }
}
