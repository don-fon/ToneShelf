import { describe, expect, it } from 'vitest'
import type { Playlist, Track } from '@music/domain'
import { emptyPlayback, readPlayback, reconcilePlayback } from './session'

const song = { id: 'song', audioVersion: 2, durationSeconds: 100, playbackStartSeconds: 5, playbackEndSeconds: 90 } as Track
const playlists: Playlist[] = [{ id: 'list', name: 'list', isDefault: true, tracks: [song, { ...song, id: 'other' }] }]
const state = () => ({ ...emptyPlayback(), playlistID: 'list', trackID: 'song', audioVersion: 2, position: 42, playing: true, recovery: { song: 0, other: 2, deleted: 3, negative: -1, invalid: NaN, huge: Infinity } })

describe('session validation', () => {
  it('preserves valid progress and random state and removes deleted or invalid entries', () => {
    expect(reconcilePlayback(state(), playlists)).toMatchObject({ position: 42, playing: true, recovery: { song: 0, other: 2 } })
    expect(reconcilePlayback(state(), [{ ...playlists[0], tracks: [song] }]).recovery).toEqual({ song: 0 })
  })
  it('resets a missing playlist, including empty libraries', () => {
    expect(reconcilePlayback({ ...state(), playlistID: 'gone' }, playlists)).toMatchObject({ playlistID: 'list', trackID: null, playing: false, position: 0, recovery: {} })
    expect(reconcilePlayback(state(), [])).toMatchObject({ playlistID: null, trackID: null, playing: false, recovery: {} })
  })
  it('clears a removed song without discarding valid playlist recovery', () => {
    expect(reconcilePlayback(state(), [{ ...playlists[0], tracks: [{ ...song, id: 'other' }] }])).toMatchObject({ trackID: null, audioVersion: null, playing: false, position: 0, recovery: { other: 2 } })
  })
  it.each([{ audioVersion: 1, position: 42 }, { audioVersion: 2, position: 0 }, { audioVersion: 2, position: 90 }, { audioVersion: 2, position: 500 }])('restores within the current audio segment: %o', (patch) => {
    expect(reconcilePlayback({ ...state(), ...patch }, playlists)).toMatchObject({ audioVersion: 2, position: 5 })
  })
  it('does not trust stored types, nonfinite values, or prototype keys', () => {
    expect(readPlayback({ position: Infinity, masterVolume: NaN, playing: 'yes', mode: 'invalid', recovery: { song: -1, other: 1.2 } })).toEqual(emptyPlayback())
    const parsed = readPlayback(JSON.parse('{"recovery":{"__proto__":0,"song":2}}'))
    expect(Object.getPrototypeOf(parsed.recovery)).toBe(Object.prototype)
    expect(parsed.recovery.song).toBe(2)
  })
})
