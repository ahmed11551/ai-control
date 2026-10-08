import type { NavigateFunction } from 'react-router-dom'
import { api } from '../lib/api'
import { clearSoftphoneStaleCrmIds, getSoftphoneOpenCardRef, setSoftphoneCrmContext } from './reportCall'
import { softphoneOwnerAllowsOpen } from './softphoneOwnerGuard'

/** Открыть карточку, привязанную к текущему softphone-звонку (лид важнее сделки). */
export async function navigateToLiveCallCard(
  navigate: NavigateFunction,
  opts: {
    leadId?: string
    dealId?: string
    openLead?: (id: string) => void
    /** Менеджер: открывать только если owner_id совпадает (защита от stale lastLeadId). */
    requireOwnerId?: string
  },
): Promise<boolean> {
  const requireOwner = (opts.requireOwnerId || '').trim()
  const lead = (opts.leadId || '').trim()
  if (lead) {
    if (requireOwner) {
      try {
        const row = await api<{ owner_id?: string }>(`/leads/${lead}`)
        if (!softphoneOwnerAllowsOpen(requireOwner, row.owner_id)) {
          clearSoftphoneStaleCrmIds()
          return false
        }
      } catch {
        clearSoftphoneStaleCrmIds()
        return false
      }
    }
    opts.openLead?.(lead)
    navigate(`/crm/leads?open=${encodeURIComponent(lead)}`)
    return true
  }
  const deal = (opts.dealId || '').trim()
  if (deal) {
    if (requireOwner) {
      try {
        const row = await api<{ owner_id?: string }>(`/deals/${deal}`)
        if (!softphoneOwnerAllowsOpen(requireOwner, row.owner_id)) {
          clearSoftphoneStaleCrmIds()
          return false
        }
      } catch {
        clearSoftphoneStaleCrmIds()
        return false
      }
    }
    navigate(`/crm?open=${encodeURIComponent(deal)}`)
    return true
  }
  return false
}

export function hasLiveCallCard(opts: { leadId?: string; dealId?: string }) {
  return Boolean((opts.leadId || '').trim() || (opts.dealId || '').trim())
}

/** Открыть карточку звонка. Если номера ещё нет в базе — создать один лид и открыть его. */
export async function openOrCreateCallCard(
  navigate: NavigateFunction,
  opts: {
    leadId?: string
    dealId?: string
    openLead?: (id: string) => void
    requireOwnerId?: string
    callId?: string
    peer?: string
  },
): Promise<boolean> {
  if (hasLiveCallCard(opts)) {
    return navigateToLiveCallCard(navigate, opts)
  }
  const ref = getSoftphoneOpenCardRef()
  // call_id только если его передали явно (список звонков). Из окна звонка
  // берём номер на экране, чтобы не открыть карточку прошлого звонка.
  const callId = (opts.callId || '').trim()
  const peer = (opts.peer || ref.peer || '').trim()
  const uid = callId ? '' : ref.uid.trim()
  if (!callId && !uid && !peer) {
    throw new Error('Нет номера клиента')
  }
  const res = await api<{ lead_id?: string; deal_id?: string }>('/calls/open-card', {
    method: 'POST',
    body: JSON.stringify({ call_id: callId, uid, peer }),
  })
  const leadId = (res.lead_id || '').trim()
  if (!leadId) throw new Error('Не удалось создать карточку')
  const dealId = (res.deal_id || '').trim()
  setSoftphoneCrmContext({ leadId, dealId })
  return navigateToLiveCallCard(navigate, { ...opts, leadId, dealId })
}
