import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  legacyShouldHideNormalIncomingBar,
  shouldHideNormalIncomingBar,
} from './incomingBarVisibility.ts'

/** Сценарий из Vox-лога: inbound на DID 79014178407, From=клиент 79259409404. */
const realInbound = {
  canAutodialUI: true,
  ringing: true,
  talking: false,
  autodialActive: false,
  suppressing: true, // окно после скорозвона / awaiting
  peerIsOurDid: false, // peer = клиент, не DID
  needsDisposition: false,
  hasSkOffer: false,
  iAmAccepter: false,
  promptReady: false,
  declinedBlock: false,
  onTelephonyPage: false,
}

describe('incomingBarVisibility', () => {
  it('LEGACY BUG: suppress alone hides real inbound ringing for managers', () => {
    assert.equal(
      legacyShouldHideNormalIncomingBar(realInbound),
      true,
      'старый код прятал бар при любом suppress',
    )
  })

  it('FIX: real inbound (client From) shows while suppress window active', () => {
    const r = shouldHideNormalIncomingBar(realInbound)
    assert.equal(r.hide, false)
    assert.equal(r.reason, null)
  })

  it('FIX: peer accidentally = DID but no autodial window → still show', () => {
    const r = shouldHideNormalIncomingBar({
      ...realInbound,
      peerIsOurDid: true,
      suppressing: false,
      autodialActive: false,
      hasSkOffer: false,
      awaitingOffer: false,
    })
    assert.equal(r.hide, false)
  })

  it('hides autodial SIP noise when peer is our DID during suppress window', () => {
    const r = shouldHideNormalIncomingBar({
      ...realInbound,
      peerIsOurDid: true,
      suppressing: true,
    })
    assert.equal(r.hide, true)
    assert.equal(r.reason, 'autodial_did_noise')
  })

  it('shows bar when not manager autodial UI', () => {
    const r = shouldHideNormalIncomingBar({
      ...realInbound,
      canAutodialUI: false,
      peerIsOurDid: true,
    })
    assert.equal(r.hide, false)
  })

  it('hides on /telephony page', () => {
    const r = shouldHideNormalIncomingBar({
      ...realInbound,
      onTelephonyPage: true,
    })
    assert.equal(r.hide, true)
    assert.equal(r.reason, 'telephony_page')
  })
})
