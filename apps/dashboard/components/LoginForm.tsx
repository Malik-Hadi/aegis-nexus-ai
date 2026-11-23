import type { FormEvent } from "react";
import { TENANTS } from "../lib/types";
import { ShieldMark } from "./Icons";

interface Props {
  error: string;
  busy: boolean;
  onSubmit: (tenantId: string, subject: string, password: string) => void;
}

export default function LoginForm({ error, busy, onSubmit }: Props) {
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    onSubmit(String(fd.get("tenant") ?? ""), String(fd.get("subject") ?? "").trim(), String(fd.get("password") ?? ""));
  };
  return (
    <div className="grid min-h-screen lg:grid-cols-[1.1fr_1fr]">
      <aside className="relative hidden flex-col justify-between overflow-hidden border-r border-[#22364b] bg-[#0d1a28] p-12 lg:flex">
        <div className="flex items-center gap-3"><ShieldMark className="h-9 w-9" /><span className="text-lg font-semibold tracking-wide">AEGIS NEXUS AI</span></div>
        <div className="max-w-md">
          <h1 className="text-4xl font-semibold leading-tight">See the incident before the alert queue fills up.</h1>
          <p className="mt-5 text-[#8fa3b8]">Agents send telemetry. Five detection rules turn matching patterns into incidents. An analyst reviews the evidence, and an admin approves any response. Every step is written to the audit log.</p>
        </div>
        <p className="text-sm text-[#5f758b]">Lab environment. Responses are simulated and never change a real host.</p>
      </aside>
      <main className="flex items-center justify-center p-6">
        <form onSubmit={submit} className="w-full max-w-sm">
          <div className="mb-6 flex items-center gap-3 lg:hidden"><ShieldMark /><span className="font-semibold tracking-wide">AEGIS NEXUS AI</span></div>
          <h2 className="text-2xl font-semibold">Sign in</h2>
          <p className="mb-5 mt-1 text-sm text-[#8fa3b8]">Use your tenant, user name and password.</p>
          {error ? <div role="alert" className="mb-4 rounded-md border border-[#ff5d73]/40 bg-[#ff5d73]/10 px-3 py-2 text-sm text-[#ff9aa8]">{error}</div> : null}
          <label htmlFor="tenant" className="mb-1 block text-sm text-[#8fa3b8]">Tenant</label>
          <select id="tenant" name="tenant" className="input">
            {TENANTS.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <label htmlFor="subject" className="mb-1 mt-4 block text-sm text-[#8fa3b8]">User</label>
          <input id="subject" name="subject" autoComplete="username" required className="input" />
          <label htmlFor="password" className="mb-1 mt-4 block text-sm text-[#8fa3b8]">Password</label>
          <input id="password" name="password" type="password" autoComplete="current-password" required className="input" />
          <button type="submit" disabled={busy} className="btn-primary mt-6 w-full py-2.5">{busy ? "Signing in…" : "Sign in"}</button>
        </form>
      </main>
    </div>
  );
}
