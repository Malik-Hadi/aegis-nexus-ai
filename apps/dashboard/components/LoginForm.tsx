import type { FormEvent } from "react";
import { TENANTS } from "../lib/types";

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
    <form onSubmit={submit} className="mx-auto mt-[12vh] max-w-sm rounded-xl border border-slate-200 bg-white p-6 dark:border-slate-800 dark:bg-slate-900">
      <div className="font-bold tracking-wide">AEGIS NEXUS AI</div>
      <p className="mb-2 text-sm text-slate-500">SOC console – lab environment</p>
      {error ? <div role="alert" className="my-2 rounded-md bg-red-100 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{error}</div> : null}
      <label htmlFor="tenant" className="mt-3 block text-sm text-slate-500">Tenant</label>
      <select id="tenant" name="tenant" className="input">
        {TENANTS.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
      </select>
      <label htmlFor="subject" className="mt-3 block text-sm text-slate-500">User</label>
      <input id="subject" name="subject" autoComplete="username" required className="input" />
      <label htmlFor="password" className="mt-3 block text-sm text-slate-500">Password</label>
      <input id="password" name="password" type="password" autoComplete="current-password" required className="input" />
      <button type="submit" disabled={busy} className="btn-primary mt-5 w-full">{busy ? "Signing in…" : "Sign in"}</button>
    </form>
  );
}
