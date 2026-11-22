import { forward } from '../../../lib/gateway.mjs'

export const dynamic = 'force-dynamic'

export async function GET(request, { params }) {
  return forward(request, (await params).parts)
}

export const POST = GET
export const PATCH = GET
