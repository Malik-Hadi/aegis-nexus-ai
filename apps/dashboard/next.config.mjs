import { fileURLToPath } from 'node:url'

export default {
  turbopack: { root: fileURLToPath(new URL('.', import.meta.url)) },
  compress: false,
  poweredByHeader: false,
  async headers() {
    return [{
      source: '/:path*',
      headers: [
        { key: 'X-Content-Type-Options', value: 'nosniff' },
        { key: 'X-Frame-Options', value: 'DENY' },
        { key: 'Referrer-Policy', value: 'no-referrer' },
      ],
    }]
  },
}
