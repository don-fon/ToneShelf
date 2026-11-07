import { describe, expect, it } from 'vitest'
import type { Track } from '@music/domain'
import { advanceRecovery, effectiveWeight, selectNext, sortTracks } from './index'

const track = (id: string, weight = 1): Track => ({
  id, title: id, artist: '', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
  durationSeconds: 10, baseRandomWeight: weight, playbackStartSeconds: 0,
  playbackEndSeconds: null, volumeRatio: 0.8, lyrics: '', audioUrl: `/audio/${id}`, audioVersion: 1, fileName: `${id}.mp3`, uploadedAt: '2026-11-08T00:00:00Z',
})
describe('weighted random policy', () => {
  it('sorts by upload instant or natural filename in both directions without mutating input', () => {
    const items = [
      { ...track('a'), fileName: '10.mp3', uploadedAt: '2026-11-08T10:00:00+08:00' },
      { ...track('b'), fileName: '2.mp3', uploadedAt: '2026-11-08T01:00:00Z' },
    ]
    expect(sortTracks(items, 'fileName', 'asc').map((item) => item.id)).toEqual(['b', 'a'])
    expect(sortTracks(items, 'fileName', 'desc').map((item) => item.id)).toEqual(['a', 'b'])
    expect(sortTracks(items, 'uploadedAt', 'asc').map((item) => item.id)).toEqual(['b', 'a'])
    expect(sortTracks(items, 'uploadedAt', 'desc').map((item) => item.id)).toEqual(['a', 'b'])
    expect(items.map((item) => item.id)).toEqual(['a', 'b'])
  })
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
