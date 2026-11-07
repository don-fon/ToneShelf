import { describe, expect, it } from 'vitest'
import type { Track } from '@music/domain'
import { advanceRecovery, effectiveWeight, selectNext } from './index'

const track = (id: string, weight = 1): Track => ({
  id, title: id, artist: '', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
  durationSeconds: 10, baseRandomWeight: weight, playbackStartSeconds: 0,
  playbackEndSeconds: null, volumeRatio: 0.8, lyrics: '', audioUrl: `/audio/${id}`,
})
describe('weighted random policy', () => {
  it('never immediately repeats when another track exists', () => {
    expect(selectNext([track('a'), track('b')], 'a', { a: 0 }, () => 0)?.id).toBe('b')
  })

  it('recovers weight over three switches', () => {
    const item = track('a', 6)
    expect(effectiveWeight(item, { a: 0 })).toBe(0)
    expect(effectiveWeight(item, { a: 1 })).toBe(2)
    expect(effectiveWeight(item, { a: 2 })).toBe(4)
    expect(effectiveWeight(item, { a: 3 })).toBe(6)
  })

  it('advances prior tracks and resets the selected track', () => {
    expect(advanceRecovery({ a: 0, b: 2 }, 'c')).toEqual({ a: 1, b: 3, c: 0 })
  })

  it('uses base weights when all effective weights are zero', () => {
    expect(selectNext([track('a'), track('b'), track('c')], 'a', { b: 0, c: 0 }, () => 0.99)?.id).toBe('c')
  })
})
