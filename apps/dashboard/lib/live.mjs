// Run immediately. If more notifications arrive during the fetch, run once more.
export function coalesce(load) {
  let active = false
  let pending = false
  return async function refresh() {
    pending = true
    if (active) return
    active = true
    try {
      while (pending) {
        pending = false
        await load()
      }
    } finally {
      active = false
    }
  }
}

export function liveResources(type) {
  switch (type) {
    case 'event.new': return ['events']
    case 'incident.created': return ['incidents']
    case 'incident.updated': return ['incidents', 'detail']
    case 'agent.heartbeat':
    case 'agent.revoked': return ['agents']
    case 'approval.requested':
    case 'approval.decided': return ['detail']
    default: return []
  }
}
