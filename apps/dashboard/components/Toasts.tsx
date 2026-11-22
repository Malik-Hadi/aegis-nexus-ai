import type { Severity } from "../lib/types";

export interface ToastItem { id: number; text: string; severity: Severity }

const BORDER: Record<Severity, string> = {
  critical: "border-l-red-500", high: "border-l-red-500", medium: "border-l-amber-500", low: "border-l-blue-500", info: "border-l-blue-500",
};

export default function Toasts({ toasts }: { toasts: ToastItem[] }) {
  return (
    <div className="fixed right-3 top-3 z-10 flex max-w-xs flex-col gap-2" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`rounded-lg border border-l-4 border-slate-200 bg-white px-3 py-2 text-sm shadow-lg dark:border-slate-700 dark:bg-slate-900 ${BORDER[t.severity] ?? BORDER.info}`}>
          {t.text}
        </div>
      ))}
    </div>
  );
}
