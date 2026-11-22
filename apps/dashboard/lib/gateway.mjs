const allowed = {
  GET: /^v1\/(me|agents|events|stream|incidents(?:\/[\w-]+(?:\/analysis\/[\w-]+)?)?)$/,
  POST: /^v1\/(auth\/(login|logout)|admin\/enrolment-tokens|agents\/[\w-]+\/revoke|incidents\/[\w-]+\/(approvals|analyse)|approvals\/[\w-]+\/decision)$/,
  PATCH: /^v1\/incidents\/[\w-]+$/,
}

function error(status, message) {
  return Response.json({ error: message }, { status, headers: { 'Cache-Control': 'no-store' } })
}

export async function forward(request, parts, env = process.env) {
  const path = ['v1', ...parts].join('/')
  if (parts.some(part => part.includes('/')) || !allowed[request.method]?.test(path)) {
    return error(404, 'Unknown dashboard endpoint')
  }
  const publicOrigin = env.SOC_PUBLIC_ORIGIN || 'http://localhost:3000'
  const writing = request.method !== 'GET'
  if (writing && request.headers.get('origin') !== publicOrigin) {
    return error(403, 'Cross-origin request refused')
  }
  try {
    const target = new URL('/' + path, env.SOC_API_ORIGIN || 'http://localhost:8080')
    target.search = new URL(request.url).search
    const headers = new Headers({ 'accept-encoding': 'identity' })
    for (const name of ['accept', 'cookie', 'content-type', 'last-event-id']) {
      const value = request.headers.get(name)
      if (value) headers.set(name, value)
    }
    // Check the browser origin above, then present the upstream's own origin to Go.
    if (writing) headers.set('origin', target.origin)

    const stream = path === 'v1/stream'
    const signal = stream
      ? request.signal
      : AbortSignal.any([request.signal, AbortSignal.timeout(10000)])
    const upstream = await fetch(target, {
      method: request.method, headers, body: writing ? request.body : undefined,
      duplex: 'half', signal, redirect: 'manual', cache: 'no-store',
    })
    const responseHeaders = new Headers({ 'Cache-Control': 'no-store' })
    for (const name of ['content-type', 'retry-after']) {
      const value = upstream.headers.get(name)
      if (value) responseHeaders.set(name, value)
    }
    for (const cookie of upstream.headers.getSetCookie()) responseHeaders.append('Set-Cookie', cookie)
    if (stream) responseHeaders.set('X-Accel-Buffering', 'no')
    return new Response(upstream.body, { status: upstream.status, headers: responseHeaders })
  } catch {
    return error(502, 'Could not reach the Go SOC API')
  }
}
