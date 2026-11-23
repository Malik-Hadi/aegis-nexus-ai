import type { Severity } from "../lib/types";

export interface ToastItem { id: number; text: string; severity: Severity }

const EDGE: Record<Severity, string> = {
  critical: "border-l-[#ff5d73]", high: "border-l-[#ff8a4c]", medium: "border-l-[#f2c14e]", low: "border-l-[#5aa9ff]", info: "border-l-[#d9ad5f]",
};

export default function Toasts({ toasts }: { toasts: ToastItem[] }) {
  return (
    <div className="fixed right-3 top-16 z-30 flex w-[calc(100%-1.5rem)] max-w-xs flex-col gap-2 md:top-3" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`rounded-lg border border-l-4 border-[#2c4560] bg-[#162739] px-3.5 py-2.5 text-sm shadow-xl ${EDGE[t.severity] ?? EDGE.info}`}>
          {t.text}
        </div>
      ))}
    </div>
  );
}
