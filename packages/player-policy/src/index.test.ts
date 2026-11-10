import { describe, expect, it } from 'vitest'
import type { Track } from '@music/domain'
import { advanceRecovery, effectiveWeight, selectNext, sortTracks } from './index'

const track = (id: string, weight = 1): Track => ({
  id, title: id, artist: '', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
  durationSeconds: 10, baseRandomWeight: weight, playbackStartSeconds: 0,
  playbackEndSeconds: null, volumeRatio: 0.8, lyrics: '', audioUrl: `/audio/${id}`, audioVersion: 1, fileName: `${id}.mp3`, uploadedAt: '2026-11-08T00:00:00Z',
})
describe('weighted random policy', () => {
  it('sorts by upload instant or current song title instead of filename without mutating input', () => {
    const items = [
      { ...track('a'), title: '歌曲10', fileName: '1.mp3', uploadedAt: '2026-11-08T10:00:00+08:00' },
      { ...track('b'), title: '歌曲2', fileName: '99.mp3', uploadedAt: '2026-11-08T01:00:00Z' },
    ]
    expect(sortTracks(items, 'title', 'asc').map((item) => item.id)).toEqual(['b', 'a'])
    expect(sortTracks(items, 'title', 'desc').map((item) => item.id)).toEqual(['a', 'b'])
    expect(sortTracks(items, 'uploadedAt', 'asc').map((item) => item.id)).toEqual(['b', 'a'])
    expect(sortTracks(items, 'uploadedAt', 'desc').map((item) => item.id)).toEqual(['a', 'b'])
    expect(items.map((item) => item.id)).toEqual(['a', 'b'])
    const renamed = items.map((item) => item.id === 'a' ? { ...item, title: '歌曲1' } : item)
    expect(sortTracks(renamed, 'title', 'asc').map((item) => item.id)).toEqual(['a', 'b'])
  })
  it('never immediately repeats when another track exists', () => {
    expect(selectNext([track('a'), track('b')], 'a', { a: 0 }, () => 0)?.id).toBe('b')
  })

  it('excludes the previous track even when its weight dominates', () => {
    const items = [track('a', 1000000), track('b'), track('c')]
    const recovery = advanceRecovery(advanceRecovery({}, 'a'), 'b')
    for (const random of [0, 0.5, 0.999999]) {
      expect(selectNext(items, 'b', recovery, () => random)?.id).toBe('c')
    }
    expect(selectNext(items, 'b', JSON.parse(JSON.stringify(recovery)), () => 0)?.id).toBe('c')
  })

  it('never produces A-B-A across a long sequence with at least three tracks', () => {
    for (const count of [3, 4, 10]) {
      const items = Array.from({ length: count }, (_, index) => track(String(index), index === 0 ? 1000000 : 1))
      let recovery = advanceRecovery({}, items[0].id)
      const played = [items[0].id]
      for (let i = 0; i < 200; i++) {
        const selected = selectNext(items, played.at(-1)!, recovery, () => (i % 11) / 11)!
        expect(selected.id).not.toBe(played.at(-1))
        expect(selected.id).not.toBe(played.at(-2))
        played.push(selected.id)
        recovery = advanceRecovery(recovery, selected.id)
      }
    }
  })

  it('keeps weighted selection among tracks outside the recent pair', () => {
    const items = [track('a', 10000), track('b'), track('c'), track('d', 3)]
    const recovery = { a: 1, b: 0 }
    expect(selectNext(items, 'b', recovery, () => 0.24)?.id).toBe('c')
    expect(selectNext(items, 'b', recovery, () => 0.26)?.id).toBe('d')
  })

  it('relaxes recent history for a two-track playlist and allows a lone track', () => {
    const recovery = advanceRecovery(advanceRecovery({}, 'a'), 'b')
    expect(selectNext([track('a'), track('b')], 'b', recovery, () => 0)?.id).toBe('a')
    expect(selectNext([track('b')], 'b', recovery)?.id).toBe('b')
    expect(selectNext([], 'b', recovery)).toBeNull()
  })

  it('does not advance the cooldown when the same track is manually selected again', () => {
    const recovery = advanceRecovery(advanceRecovery({}, 'a'), 'b')
    expect(advanceRecovery(recovery, 'b')).toEqual(recovery)
    expect(recovery).toEqual({ a: 1, b: 0 })
    const next = advanceRecovery(recovery, 'c')
    expect(next).toEqual({ a: 2, b: 1, c: 0 })
    expect(selectNext([track('a'), track('b'), track('c')], 'c', next, () => 0)?.id).toBe('a')
  })

  it.each([2, 4, 5, 10, 100])('recovers linearly over half of a %i-track playlist', (size) => {
    const item = track('a', 6)
    for (let steps = 0; steps <= Math.ceil(size / 2) + 2; steps++) {
      expect(effectiveWeight(item, { a: steps }, size)).toBeCloseTo(6 * Math.min(1, steps / (size / 2)))
    }
    expect(effectiveWeight(item, {}, size)).toBe(6)
  })

  it('uses a fractional threshold for odd playlists and safely handles a single track', () => {
    expect(effectiveWeight(track('a'), { a: 2 }, 5)).toBeCloseTo(0.8)
    expect(effectiveWeight(track('a'), { a: 3 }, 5)).toBe(1)
    expect(effectiveWeight(track('a'), { a: 0 }, 1)).toBe(0)
    expect(effectiveWeight(track('a'), { a: 1 }, 1)).toBe(1)
  })

  it('retains counts past three switches and adapts to playlist size changes after refresh', () => {
    let recovery = advanceRecovery({}, 'a')
    for (let i = 0; i < 8; i++) recovery = advanceRecovery(recovery, `other-${i}`)
    expect(recovery.a).toBe(8)
    const restored = JSON.parse(JSON.stringify(recovery))
    expect(effectiveWeight(track('a'), restored, 20)).toBeCloseTo(0.8)
    expect(effectiveWeight(track('a'), restored, 10)).toBe(1)
    expect(effectiveWeight(track('a'), restored, 40)).toBeCloseTo(0.4)
  })

  it('uses the entire playlist size rather than the filtered candidate count', () => {
    const items = Array.from({ length: 10 }, (_, i) => track(String(i)))
    expect(selectNext(items, '9', { '0': 2, '9': 0 }, () => 0.08)?.id).toBe('1')
  })

  it('advances prior tracks and resets the selected track', () => {
    expect(advanceRecovery({ a: 0, b: 2 }, 'c')).toEqual({ a: 1, b: 3, c: 0 })
  })

  it('uses base weights when all effective weights are zero', () => {
    expect(selectNext([track('a'), track('b'), track('c')], 'a', { b: 0, c: 0 }, () => 0.99)?.id).toBe('c')
  })
})
