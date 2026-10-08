import type { SoftphoneStatus } from './types'

function inFlight(status: SoftphoneStatus) {
  return status === 'dialing' || status === 'ringing' || status === 'active'
}

/** Сид CRM ids для нового bridge — не затираем last* пустым pending. */
export function seedBridgeCrmIds(opts: {
  direction: 'incoming' | 'outgoing'
  pendingLeadId: string
  pendingDealId: string
  lastLeadId: string
  lastDealId: string
}): { leadId?: string; dealId?: string; lastLeadId: string; lastDealId: string } {
  const seedLead =
    opts.pendingLeadId || (opts.direction === 'outgoing' ? opts.lastLeadId : '') || undefined
  const seedDeal =
    opts.pendingDealId || (opts.direction === 'outgoing' ? opts.lastDealId : '') || undefined
  return {
    leadId: seedLead,
    dealId: seedDeal,
    lastLeadId: opts.pendingLeadId || opts.lastLeadId,
    lastDealId: opts.pendingDealId || opts.lastDealId,
  }
}

/**
 * Какие lead/deal слать в softphone-event.
 * Входящий: только явный pending (открытая карточка) — НЕ last* от прошлого звонка
 * (иначе колбэк Джатдая вешается на Дмитрия).
 * Исходящий: pending → bridge → last (менеджер звонит из карточки).
 */
export function crmIdsForSoftphoneEvent(opts: {
  direction: 'incoming' | 'outgoing'
  bridgeLeadId?: string
  bridgeDealId?: string
  pendingLeadId: string
  pendingDealId: string
  lastLeadId: string
  lastDealId: string
}): { lead_id?: string; deal_id?: string } {
  const out: { lead_id?: string; deal_id?: string } = {}
  if (opts.direction === 'incoming') {
    const lead = (opts.pendingLeadId || opts.bridgeLeadId || '').trim()
    const deal = (opts.pendingDealId || opts.bridgeDealId || '').trim()
    // bridge на inbound без pending не сидится last* — но если уже ошибочно залип, игнорим last*
    if (lead) out.lead_id = lead
    else if (deal) out.deal_id = deal
    return out
  }
  const lead = (opts.bridgeLeadId || opts.pendingLeadId || opts.lastLeadId || '').trim()
  const deal = (opts.bridgeDealId || opts.pendingDealId || opts.lastDealId || '').trim()
  if (lead) out.lead_id = lead
  else if (deal) out.deal_id = deal
  return out
}

/** Для store: не затирать live id пустой строкой во время звонка. */
export function mergeLiveCrmIds(
  prev: { leadId: string; dealId: string },
  next: { leadId: string; dealId: string },
  status: SoftphoneStatus,
): { leadId: string; dealId: string } {
  const keep = inFlight(status)
  return {
    leadId: next.leadId || (keep ? prev.leadId : next.leadId),
    dealId: next.dealId || (keep ? prev.dealId : next.dealId),
  }
}
