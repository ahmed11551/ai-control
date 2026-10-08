import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { dealBelongsOnBoard, shouldSkipForeignWsDeal } from './dealBoardAcl.ts'

describe('dealBelongsOnBoard', () => {
  it('manager: only own owner_id', () => {
    const opts = { userId: 'me', isManager: true }
    assert.equal(dealBelongsOnBoard({ owner_id: 'me' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_id: 'other' }, opts), false)
    assert.equal(dealBelongsOnBoard({}, opts), false)
    assert.equal(dealBelongsOnBoard({ owner_id: '' }, opts), false)
  })

  it('mine filter same as manager', () => {
    const opts = { userId: 'me', mine: true }
    assert.equal(dealBelongsOnBoard({ owner_id: 'me' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_id: 'x' }, opts), false)
  })

  it('no user → never on board', () => {
    assert.equal(dealBelongsOnBoard({ owner_id: 'me' }, { isManager: true }), false)
  })

  it('admin + dept filter', () => {
    const opts = { userId: 'admin', isAdmin: true, deptId: 'd1' }
    assert.equal(dealBelongsOnBoard({ owner_department_id: 'd1' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_department_id: 'd2' }, opts), false)
    assert.equal(dealBelongsOnBoard({ owner_id: 'anyone' }, opts), false)
  })

  it('rop/admin without dept filter → pass through (server ACL)', () => {
    assert.equal(
      dealBelongsOnBoard({ owner_id: 'colleague' }, { userId: 'rop', isAdmin: false }),
      true,
    )
  })

  it('nedozvon pool owned by other must not land on manager board', () => {
    // Даже если GET /deals/:id без for_board отдал бы 200.
    assert.equal(
      dealBelongsOnBoard({ owner_id: 'pool-owner' }, { userId: 'me', isManager: true }),
      false,
    )
  })
})

describe('shouldSkipForeignWsDeal', () => {
  it('skips foreign owner for manager before fetch', () => {
    assert.equal(
      shouldSkipForeignWsDeal({
        isManager: true,
        userId: 'me',
        payloadOwnerId: 'other',
      }),
      true,
    )
  })

  it('does not skip own or missing owner (need GET)', () => {
    assert.equal(
      shouldSkipForeignWsDeal({ isManager: true, userId: 'me', payloadOwnerId: 'me' }),
      false,
    )
    assert.equal(
      shouldSkipForeignWsDeal({ isManager: true, userId: 'me', payloadOwnerId: '' }),
      false,
    )
  })

  it('manager on sleeping board sees only cards already given to him', () => {
    const opts = { userId: 'me', isManager: true, sharedBoard: false }
    assert.equal(dealBelongsOnBoard({ owner_id: 'me' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_id: '' }, opts), false)
    assert.equal(dealBelongsOnBoard({ owner_id: 'other' }, opts), false)
    assert.equal(
      shouldSkipForeignWsDeal({
        isManager: true,
        userId: 'me',
        payloadOwnerId: 'other',
        sharedBoard: false,
      }),
      true,
    )
  })

  it('shared old-leads board keeps unassigned cards', () => {
    const opts = { userId: 'me', isManager: true, sharedBoard: true }
    assert.equal(dealBelongsOnBoard({ owner_id: '' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_id: 'other' }, opts), true)
    assert.equal(dealBelongsOnBoard({ owner_id: 'me' }, { ...opts, mine: true }), true)
    assert.equal(dealBelongsOnBoard({ owner_id: 'other' }, { ...opts, mine: true }), false)
    assert.equal(
      shouldSkipForeignWsDeal({
        isManager: true,
        userId: 'me',
        payloadOwnerId: 'other',
        sharedBoard: true,
      }),
      false,
    )
  })
  it('rop/admin without mine do not skip by owner', () => {
    assert.equal(
      shouldSkipForeignWsDeal({
        isManager: false,
        userId: 'rop',
        payloadOwnerId: 'other',
      }),
      false,
    )
  })
})
