import { api } from './api'

/** Resolve CRM lead id from a task/entity link (lead or deal). */
export async function resolveLeadIdFromEntity(
  entityType?: string,
  entityId?: string,
): Promise<string | null> {
  const type = String(entityType || '').toLowerCase()
  const id = String(entityId || '').trim()
  if (!id) return null
  if (type === 'lead') return id
  if (type === 'deal') {
    try {
      const deal = await api<{ lead_id?: string }>(`/deals/${id}`)
      const leadId = String(deal.lead_id || '').trim()
      return leadId || null
    } catch {
      return null
    }
  }
  return null
}
