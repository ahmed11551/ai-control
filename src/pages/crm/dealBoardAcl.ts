/** ACL канбана менеджера — отдельно от canSeeDeal (недозвон/пул). */

export type BoardDeal = {
  owner_id?: string
  owner_department_id?: string
}

export type BoardAclOpts = {
  userId?: string
  mine?: boolean
  isManager?: boolean
  isAdmin?: boolean
  deptId?: string
  /** Общая доска отделения: видны и карточки без ответственного. */
  sharedBoard?: boolean
}

export function dealBelongsOnBoard(deal: BoardDeal, opts: BoardAclOpts): boolean {
  const uid = (opts.userId || '').trim()
  if (!uid) return false
  if (opts.sharedBoard) {
    if (opts.mine) return Boolean(deal.owner_id) && deal.owner_id === uid
    return true
  }
  if (opts.mine || opts.isManager) {
    return Boolean(deal.owner_id) && deal.owner_id === uid
  }
  if (opts.isAdmin && opts.deptId) {
    return Boolean(deal.owner_department_id) && deal.owner_department_id === opts.deptId
  }
  return true
}

/** WS: чужой owner_id в payload — сразу убрать с доски, без GET. */
export function shouldSkipForeignWsDeal(opts: {
  mine?: boolean
  isManager?: boolean
  userId?: string
  payloadOwnerId?: string
  sharedBoard?: boolean
}): boolean {
  if (opts.sharedBoard) return false
  if (!(opts.mine || opts.isManager)) return false
  const uid = (opts.userId || '').trim()
  const owner = (opts.payloadOwnerId || '').trim()
  if (!uid || !owner) return false
  return owner !== uid
}
