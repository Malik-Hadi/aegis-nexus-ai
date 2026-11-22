import { randomBytes } from 'node:crypto'
import { NextResponse } from 'next/server'

export function proxy(request) {
  const nonce = randomBytes(16).toString('base64')
  const dev = process.env.NODE_ENV === 'development'
  const policy = [
    "default-src 'none'",
    "script-src 'self' 'nonce-" + nonce + "'" + (dev ? " 'unsafe-eval'" : ""),
    "style-src 'self' 'nonce-" + nonce + "'" + (dev ? " 'unsafe-inline'" : ""),
    "connect-src 'self'",
    "img-src 'self' data:",
    "font-src 'self'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ')
  const headers = new Headers(request.headers)
  headers.set('Content-Security-Policy', policy)
  const response = NextResponse.next({ request: { headers } })
  response.headers.set('Content-Security-Policy', policy)
  return response
}

export const config = { matcher: ['/'] }
