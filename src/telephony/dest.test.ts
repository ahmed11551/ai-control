import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { dialProvider, isCityPhone, normalizeVoxDest } from './dest.ts'

describe('dialProvider', () => {
  it('keeps Voximplant when dialing a city number', () => {
    assert.equal(dialProvider('vox', '7 925 123-45-67'), 'vox')
    assert.equal(dialProvider('vox', '79251234567'), 'vox')
    assert.equal(dialProvider('vox', '+79251234567'), 'vox')
  })

  it('keeps Exolve only when that tab is already selected', () => {
    assert.equal(dialProvider('exolve', '79251234567'), 'exolve')
  })
})

describe('normalizeVoxDest', () => {
  it('strips spaces and dashes from Russian mobiles', () => {
    assert.equal(normalizeVoxDest('7 925 123-45-67'), '+79251234567')
    assert.equal(normalizeVoxDest('8 (925) 123-45-67'), '+79251234567')
  })

  it('leaves internal Vox users unchanged', () => {
    assert.equal(normalizeVoxDest('agent2'), 'agent2')
  })
})

describe('isCityPhone', () => {
  it('detects 7925… and rejects agent names', () => {
    assert.equal(isCityPhone('7 925 0000000'), true)
    assert.equal(isCityPhone('agent2'), false)
  })
})
