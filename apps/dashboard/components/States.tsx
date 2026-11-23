import type { ReactNode } from "react";

/** An empty screen tells the person what to do next. */
export function Empty({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
      <p className="text-sm font-semibold">{title}</p>
      {children ? <p className="max-w-md text-sm text-[#8fa3b8]">{children}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3 p-4" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => <div key={i} className="skeleton h-9 w-full" />)}
    </div>
  );
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return <div role="alert" className="mb-4 rounded-md border border-[#ff5d73]/40 bg-[#ff5d73]/10 px-3 py-2 text-sm text-[#ff9aa8]">{children}</div>;
}
