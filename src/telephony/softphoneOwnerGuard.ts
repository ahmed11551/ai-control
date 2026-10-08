/** Softphone: не открывать чужой stale lastLeadId у менеджера. */

export function softphoneOwnerAllowsOpen(
  requireOwnerId: string | undefined,
  entityOwnerId: string | undefined,
): boolean {
  const require = (requireOwnerId || '').trim()
  if (!require) return true
  const owner = (entityOwnerId || '').trim()
  // Пустой owner (банк) — не блокируем на этом слое; 404/ACL дальше.
  if (!owner) return true
  return owner === require
}
