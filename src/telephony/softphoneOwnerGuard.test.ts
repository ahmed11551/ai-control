import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { softphoneOwnerAllowsOpen } from './softphoneOwnerGuard.ts'

describe('softphoneOwnerAllowsOpen', () => {
  it('without requireOwner — always allow (autodial pool)', () => {
    assert.equal(softphoneOwnerAllowsOpen(undefined, 'other'), true)
    assert.equal(softphoneOwnerAllowsOpen('', 'other'), true)
  })

  it('manager: blocks foreign owner (stale lastLeadId)', () => {
    assert.equal(softphoneOwnerAllowsOpen('me', 'other'), false)
    assert.equal(softphoneOwnerAllowsOpen('me', 'me'), true)
  })

  it('empty entity owner does not block', () => {
    assert.equal(softphoneOwnerAllowsOpen('me', ''), true)
    assert.equal(softphoneOwnerAllowsOpen('me', undefined), true)
  })

  it('trims whitespace', () => {
    assert.equal(softphoneOwnerAllowsOpen(' me ', ' me '), true)
    assert.equal(softphoneOwnerAllowsOpen(' me ', ' other '), false)
  })
})
