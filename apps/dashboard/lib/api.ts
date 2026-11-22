export interface ApiResult<T> { ok: boolean; status: number; data: T | null; error: string }

let unauthorizedHandler: (() => void) | null = null;
/** Called when any API call returns 401 while signed in (the login expired). */
export function setUnauthorizedHandler(fn: (() => void) | null): void { unauthorizedHandler = fn; }

function errorText(json: unknown, status: number): string {
  if (typeof json === "object" && json !== null && "error" in json) {
    const e = (json as { error?: unknown }).error;
    if (typeof e === "string") return e;
  }
  return `request failed (${status})`;
}

// Same-origin calls only. The session cookie is HttpOnly, so this code never sees it.
export async function api<T = unknown>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<ApiResult<T>> {
  const init: RequestInit = { method, credentials: "same-origin", signal };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  const handleUnauthorized = unauthorizedHandler;
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch {
    return { ok: false, status: 0, data: null, error: "network error" };
  }
  let json: unknown = null;
  try { json = await res.json(); } catch { json = null; }
  if (!signal?.aborted && res.status === 401 && handleUnauthorized === unauthorizedHandler) handleUnauthorized?.();
  return { ok: res.ok, status: res.status, data: res.ok ? (json as T) : null, error: res.ok ? "" : errorText(json, res.status) };
}
