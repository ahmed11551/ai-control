import { api } from '../lib/api'
import { pushCallHistory } from './callHistory'
import { resolveCallReach } from './callDisposition'
import { crmIdsForSoftphoneEvent, seedBridgeCrmIds } from './reportCallPure'
import type { CallHistoryItem, SoftphoneSnapshot, SoftphoneStatus } from './types'

export { crmIdsForSoftphoneEvent, mergeLiveCrmIds, seedBridgeCrmIds } from './reportCallPure'

export type SoftphoneCrmEvent = {
  uid: string
  provider: 'vox' | 'exolve'
  direction: 'incoming' | 'outgoing'
  peer: string
  phase: 'start' | 'end'
  duration_sec: number
  disposition?: string
  cli?: string
  lead_id?: string
  deal_id?: string
}

export type SoftphoneCrmResult = {
  call_id?: string
  deal_id?: string
  lead_id?: string
}

type Bridge = {
  uid: string
  peer: string
  direction: 'incoming' | 'outgoing'
  startedAt: string
  maxDuration: number
  startSent: boolean
  cli?: string
  leadId?: string
  dealId?: string
}

let bridge: Bridge | null = null
let prevStatus: SoftphoneStatus = 'idle'
let lastDealId = ''
let lastLeadId = ''
let lastError = ''
let activeCli = ''
let pendingLeadId = ''
let pendingDealId = ''
let openCardRef: { uid: string; peer: string; callId: string } = { uid: '', peer: '', callId: '' }

/** Текущий звонок для кнопки «Открыть карточку». Не сбрасывается в момент завершения. */
export function getSoftphoneOpenCardRef() {
  return { ...openCardRef }
}

function rememberOpenCard(next: Partial<{ uid: string; peer: string; callId: string }>) {
  openCardRef = { ...openCardRef, ...next }
}

/** Сериализация: параллельные snap-эффекты не портят bridge/CRM ids. */
let journalChain: Promise<void> = Promise.resolve()

/** Привязать следующий/текущий softphone-звонок к карточке CRM. */
export function setSoftphoneCrmContext(ctx: { leadId?: string; dealId?: string } | null) {
  pendingLeadId = (ctx?.leadId || '').trim()
  pendingDealId = (ctx?.dealId || '').trim()
  if (bridge) {
    if (pendingLeadId) bridge.leadId = pendingLeadId
    if (pendingDealId) bridge.dealId = pendingDealId
  }
  if (pendingLeadId) lastLeadId = pendingLeadId
  if (pendingDealId) lastDealId = pendingDealId
}

export function getSoftphoneCrmContext() {
  return { leadId: pendingLeadId, dealId: pendingDealId }
}

/** Remember CLI for the current outbound softphone call (Exolve carousel). */
export function setSoftphoneActiveCli(cli: string) {
  activeCli = cli.trim()
  if (bridge && activeCli) bridge.cli = activeCli
}

export function getSoftphoneActiveCli() {
  return activeCli
}

export function newCallUid() {
  return `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
}

export function reportSoftphone(body: SoftphoneCrmEvent) {
  return api<SoftphoneCrmResult>('/telephony/softphone-event', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

function inFlight(status: SoftphoneStatus) {
  return status === 'dialing' || status === 'ringing' || status === 'active'
}

function crmFields(b: Bridge) {
  return crmIdsForSoftphoneEvent({
    direction: b.direction,
    bridgeLeadId: b.leadId,
    bridgeDealId: b.dealId,
    pendingLeadId,
    pendingDealId,
    lastLeadId,
    lastDealId,
  })
}

/** Сброс stale CRM ids (чужой lastLead после смены звонка). */
export function clearSoftphoneStaleCrmIds() {
  lastDealId = ''
  lastLeadId = ''
  pendingLeadId = ''
  pendingDealId = ''
  if (bridge) {
    bridge.leadId = undefined
    bridge.dealId = undefined
  }
}

/** Сброс модуля для unit-тестов. */
export function resetSoftphoneJournalForTests() {
  bridge = null
  prevStatus = 'idle'
  lastDealId = ''
  lastLeadId = ''
  lastError = ''
  activeCli = ''
  pendingLeadId = ''
  pendingDealId = ''
  journalChain = Promise.resolve()
  openCardRef = { uid: '', peer: '', callId: '' }
}

export function getSoftphoneJournalSnapshotForTests() {
  return {
    lastLeadId,
    lastDealId,
    pendingLeadId,
    pendingDealId,
    bridgeLeadId: bridge?.leadId || '',
    bridgeDirection: bridge?.direction || '',
    prevStatus,
  }
}

async function syncSoftphoneJournalInner(
  provider: 'vox' | 'exolve',
  snap: SoftphoneSnapshot,
): Promise<{ dealId: string; leadId: string; error: string; ended?: CallHistoryItem }> {
  let ended: CallHistoryItem | undefined

  if (inFlight(snap.status)) {
    const peer =
      snap.status === 'ringing'
        ? snap.incomingPeer || snap.peer || bridge?.peer || ''
        : snap.peer || bridge?.peer || ''
    const direction: 'incoming' | 'outgoing' =
      snap.status === 'ringing' ? 'incoming' : bridge?.direction || 'outgoing'
    if (!bridge) {
      // Не обнуляем last* — иначе после leftCall + новый ringing карточка лида пропадает.
      const seeded = seedBridgeCrmIds({
        direction,
        pendingLeadId,
        pendingDealId,
        lastLeadId,
        lastDealId,
      })
      bridge = {
        uid: newCallUid(),
        peer,
        direction,
        startedAt: new Date().toISOString(),
        maxDuration: 0,
        startSent: false,
        cli: activeCli || undefined,
        leadId: seeded.leadId,
        dealId: seeded.dealId,
      }
      rememberOpenCard({ uid: bridge.uid, peer: bridge.peer, callId: '' })
      lastLeadId = seeded.lastLeadId
      lastDealId = seeded.lastDealId
      lastError = ''
    } else {
      if (peer) bridge.peer = peer
      if (bridge.peer) rememberOpenCard({ peer: bridge.peer })
      bridge.direction = direction
      if (activeCli) bridge.cli = activeCli
      if (pendingLeadId) bridge.leadId = pendingLeadId
      if (pendingDealId) bridge.dealId = pendingDealId
    }
    bridge.maxDuration = Math.max(bridge.maxDuration, snap.durationSec)
    if (!bridge.startSent && bridge.peer && bridge.peer !== 'unknown') {
      bridge.startSent = true
      try {
        const res = await reportSoftphone({
          uid: bridge.uid,
          provider,
          direction: bridge.direction,
          peer: bridge.peer,
          phase: 'start',
          duration_sec: 0,
          cli: bridge.cli,
          ...crmFields(bridge),
        })
        if (res.deal_id) lastDealId = res.deal_id
        if (res.lead_id) lastLeadId = res.lead_id
        if (res.call_id) rememberOpenCard({ callId: res.call_id })
        lastError = ''
      } catch (e) {
        lastError = e instanceof Error ? e.message : 'Не удалось записать звонок в CRM'
        bridge.startSent = false
      }
    }
  }

  const leftCall =
    (prevStatus === 'active' || prevStatus === 'dialing' || prevStatus === 'ringing') &&
    (snap.status === 'ready' || snap.status === 'idle' || snap.status === 'error')

  if (leftCall && bridge) {
    const session = bridge
    const durationSec = session.maxDuration
    const reach = resolveCallReach(durationSec)
    const status = reach === 'reached' ? 'reached' : 'no_reach'
    if (session.peer && session.peer !== 'unknown') {
      try {
        const res = await reportSoftphone({
          uid: session.uid,
          provider,
          direction: session.direction,
          peer: session.peer,
          phase: 'end',
          duration_sec: durationSec,
          disposition: status,
          cli: session.cli,
          ...crmFields(session),
        })
        if (res.deal_id) lastDealId = res.deal_id
        if (res.lead_id) lastLeadId = res.lead_id
        if (res.call_id) rememberOpenCard({ callId: res.call_id })
        if (!lastError) lastError = ''
      } catch (e) {
        lastError = e instanceof Error ? e.message : 'Не удалось записать звонок в CRM'
      }
    }
    ended = {
      id: `call_${session.uid}`,
      direction: session.direction,
      peer: session.peer,
      startedAt: session.startedAt,
      endedAt: new Date().toISOString(),
      durationSec,
      status: reach === 'reached' ? 'answered' : session.direction === 'incoming' ? 'missed' : 'cancelled',
    }
    pushCallHistory(ended)
    bridge = null
    activeCli = ''
    // pending сбрасываем; lastLeadId/lastDealId оставляем — кнопка «карточка» и защита от wipe
    pendingLeadId = ''
    pendingDealId = ''
  }

  prevStatus = snap.status
  return { dealId: lastDealId, leadId: lastLeadId, error: lastError, ended }
}

export function syncSoftphoneJournal(
  provider: 'vox' | 'exolve',
  snap: SoftphoneSnapshot,
): Promise<{ dealId: string; leadId: string; error: string; ended?: CallHistoryItem }> {
  const run = () => syncSoftphoneJournalInner(provider, snap)
  const p = journalChain.then(run, run)
  journalChain = p.then(
    () => undefined,
    () => undefined,
  )
  return p
}
