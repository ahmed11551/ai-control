import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  applyCallFailedIsolation,
  inboundRejectMode,
  shouldRejectInboundBusy,
  softphoneIconLive,
  softphonePhaseHint,
  softphonePhaseLabel,
} from './callPhase.ts'
import { crmIdsForSoftphoneEvent, mergeLiveCrmIds, seedBridgeCrmIds } from './reportCallPure.ts'

describe('inboundRejectMode', () => {
  it('Busy only while dialing or active', () => {
    assert.equal(
      inboundRejectMode({ status: 'dialing', hasActiveCall: true, hasIncomingCall: false }),
      'busy',
    )
    assert.equal(
      inboundRejectMode({ status: 'active', hasActiveCall: true, hasIncomingCall: false }),
      'busy',
    )
  })

  it('Decline only when another incoming is already held', () => {
    assert.equal(
      inboundRejectMode({
        status: 'connecting',
        hasActiveCall: false,
        hasIncomingCall: false,
      }),
      null,
    )
    assert.equal(
      inboundRejectMode({ status: 'ringing', hasActiveCall: false, hasIncomingCall: false }),
      null,
    )
    assert.equal(
      inboundRejectMode({
        status: 'ringing',
        hasActiveCall: false,
        hasIncomingCall: true,
        incomingCallId: 'a',
        newCallId: 'b',
      }),
      'decline',
    )
  })

  it('ignores duplicate IncomingCall for the same id', () => {
    assert.equal(
      inboundRejectMode({
        status: 'ringing',
        hasActiveCall: false,
        hasIncomingCall: true,
        incomingCallId: 'same',
        newCallId: 'same',
      }),
      null,
    )
  })

  it('allows inbound when ready and idle', () => {
    assert.equal(
      inboundRejectMode({ status: 'ready', hasActiveCall: false, hasIncomingCall: false }),
      null,
    )
    assert.equal(
      shouldRejectInboundBusy({ status: 'ready', hasActiveCall: false, hasIncomingCall: false }),
      false,
    )
  })
})

describe('applyCallFailedIsolation', () => {
  it('keeps outbound when inbound call id fails', () => {
    const next = applyCallFailedIsolation(
      {
        activeCallId: 'out-1',
        incomingCallId: 'in-2',
        status: 'active',
        peer: '+79001112233',
        incomingPeer: '+79004445566',
      },
      'in-2',
    )
    assert.equal(next.activeCallId, 'out-1')
    assert.equal(next.incomingCallId, null)
    assert.equal(next.status, 'active')
    assert.equal(next.peer, '+79001112233')
  })

  it('clears only failed outbound', () => {
    const next = applyCallFailedIsolation(
      {
        activeCallId: 'out-1',
        incomingCallId: null,
        status: 'dialing',
        peer: '+79001112233',
      },
      'out-1',
    )
    assert.equal(next.activeCallId, null)
    assert.equal(next.status, 'ready')
  })

  it('ignores stale failed id', () => {
    const state = {
      activeCallId: 'out-1',
      incomingCallId: null as string | null,
      status: 'active' as const,
      peer: 'x',
    }
    const next = applyCallFailedIsolation(state, 'other')
    assert.deepEqual(next, state)
  })
})

describe('softphonePhaseLabel', () => {
  it('distinguishes dialing from active', () => {
    assert.equal(softphonePhaseLabel('dialing'), 'Набор…')
    assert.equal(softphonePhaseLabel('active'), 'Разговор')
    assert.equal(softphonePhaseLabel('ready', true), 'Завершён')
    assert.equal(softphonePhaseHint('dialing'), 'гудки / соединение')
    assert.equal(softphonePhaseHint('active'), null)
  })

  it('marks connecting as live icon', () => {
    assert.equal(softphoneIconLive('connecting'), true)
    assert.equal(softphoneIconLive('idle'), false)
  })
})

describe('mergeLiveCrmIds', () => {
  it('keeps previous lead id while dialing if next empty', () => {
    const m = mergeLiveCrmIds(
      { leadId: 'lead-a', dealId: '' },
      { leadId: '', dealId: '' },
      'dialing',
    )
    assert.equal(m.leadId, 'lead-a')
  })

  it('allows clear when idle', () => {
    const m = mergeLiveCrmIds(
      { leadId: 'lead-a', dealId: 'deal-a' },
      { leadId: '', dealId: '' },
      'ready',
    )
    assert.equal(m.leadId, '')
    assert.equal(m.dealId, '')
  })
})

describe('seedBridgeCrmIds', () => {
  it('keeps lastLeadId for outgoing when pending empty', () => {
    const s = seedBridgeCrmIds({
      direction: 'outgoing',
      pendingLeadId: '',
      pendingDealId: '',
      lastLeadId: 'lead-keep',
      lastDealId: '',
    })
    assert.equal(s.leadId, 'lead-keep')
    assert.equal(s.lastLeadId, 'lead-keep')
  })

  it('does not seed lastLead onto inbound bridge but keeps lastLeadId', () => {
    const s = seedBridgeCrmIds({
      direction: 'incoming',
      pendingLeadId: '',
      pendingDealId: '',
      lastLeadId: 'lead-keep',
      lastDealId: '',
    })
    assert.equal(s.leadId, undefined)
    assert.equal(s.lastLeadId, 'lead-keep')
  })
})

describe('crmIdsForSoftphoneEvent', () => {
  it('inbound ignores stale lastLeadId (Джатдай vs Дмитрий)', () => {
    const ids = crmIdsForSoftphoneEvent({
      direction: 'incoming',
      bridgeLeadId: undefined,
      pendingLeadId: '',
      pendingDealId: '',
      lastLeadId: 'dmitry-stale',
      lastDealId: '',
    })
    assert.equal(ids.lead_id, undefined)
    assert.equal(ids.deal_id, undefined)
  })

  it('inbound uses pending when manager has that card open', () => {
    const ids = crmIdsForSoftphoneEvent({
      direction: 'incoming',
      pendingLeadId: 'jatdai',
      pendingDealId: '',
      lastLeadId: 'dmitry-stale',
      lastDealId: '',
    })
    assert.equal(ids.lead_id, 'jatdai')
  })

  it('outbound still falls back to lastLeadId', () => {
    const ids = crmIdsForSoftphoneEvent({
      direction: 'outgoing',
      pendingLeadId: '',
      pendingDealId: '',
      lastLeadId: 'from-card',
      lastDealId: '',
    })
    assert.equal(ids.lead_id, 'from-card')
  })
})
