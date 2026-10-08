/**
 * Когда обычный IncomingCallBar (справа внизу) должен молчать.
 * Скорозвон: INVITE с From=наш DID во время/после autodial — глушим.
 * Реальный входящий (From=клиент) — всегда показываем.
 * Peer по ошибке = наш DID, но autodial не идёт — тоже показываем.
 */
export function shouldHideNormalIncomingBar(opts: {
  canAutodialUI: boolean
  ringing: boolean
  talking: boolean
  autodialActive: boolean
  suppressing: boolean
  awaitingOffer?: boolean
  peerIsOurDid: boolean
  needsDisposition: boolean
  hasSkOffer: boolean
  iAmAccepter: boolean
  promptReady: boolean
  declinedBlock: boolean
  onTelephonyPage: boolean
}): { hide: boolean; reason: string | null } {
  if (opts.onTelephonyPage) {
    return { hide: true, reason: 'telephony_page' }
  }

  const skorozvonTalk =
    opts.canAutodialUI &&
    opts.talking &&
    opts.iAmAccepter &&
    (opts.autodialActive || opts.hasSkOffer)

  const autodialWindow =
    opts.autodialActive ||
    opts.suppressing ||
    Boolean(opts.awaitingOffer) ||
    opts.hasSkOffer

  // DID-шум только в окне скорозвона — иначе «peer=DID» не прячет обычный входящий.
  const suppressNoise = opts.canAutodialUI && opts.peerIsOurDid && autodialWindow

  const autodialContext =
    opts.canAutodialUI &&
    (opts.needsDisposition ||
      skorozvonTalk ||
      suppressNoise ||
      (opts.autodialActive && opts.hasSkOffer))

  const showAutodialRing =
    autodialContext &&
    opts.promptReady &&
    !opts.declinedBlock &&
    !opts.needsDisposition &&
    opts.ringing &&
    !opts.talking
  const showAutodialTalk =
    skorozvonTalk && (!opts.declinedBlock || opts.iAmAccepter) && !opts.needsDisposition

  if (opts.needsDisposition && opts.canAutodialUI) {
    return { hide: false, reason: 'disposition_modal' }
  }
  if ((showAutodialRing || showAutodialTalk) && opts.canAutodialUI) {
    return { hide: false, reason: 'autodial_modal' }
  }
  if (autodialContext && !opts.talking) {
    return { hide: true, reason: opts.peerIsOurDid ? 'autodial_did_noise' : 'autodial_context' }
  }
  return { hide: false, reason: null }
}

/** Legacy-баг: suppress/autodialActive без проверки DID прятал любой входящий. */
export function legacyShouldHideNormalIncomingBar(opts: {
  canAutodialUI: boolean
  ringing: boolean
  talking: boolean
  autodialActive: boolean
  suppressing: boolean
  peerIsOurDid: boolean
  needsDisposition: boolean
  hasSkOffer: boolean
}): boolean {
  const suppressNoise =
    opts.canAutodialUI && (opts.autodialActive || opts.suppressing || opts.peerIsOurDid)
  const autodialContext =
    opts.canAutodialUI &&
    (opts.needsDisposition || suppressNoise || (opts.autodialActive && opts.hasSkOffer))
  return Boolean(autodialContext && !opts.talking)
}
