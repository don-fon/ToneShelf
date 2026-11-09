import { afterEach, describe, expect, it, vi } from 'vitest'
import { emptyPlayback } from '@music/player-policy'
import { createPlaybackPersistence, playbackStorageKey, PLAYBACK_SAVE_INTERVAL, watchSessionLifecycle } from './session'

afterEach(() => { vi.useRealTimers(); sessionStorage.clear(); vi.restoreAllMocks() })
const user = { id: 'alice', isDefault: false }
const snapshot = () => JSON.parse(sessionStorage.getItem(playbackStorageKey(user.id))!)

describe('playback persistence', () => {
  it('throttles continuous progress, saves the latest value, and flushes critical changes', () => {
    vi.useFakeTimers()
    const persistence = createPlaybackPersistence(sessionStorage, Date.now, () => 'session-a')
    persistence.open(user)
    const writes = vi.spyOn(Storage.prototype, 'setItem')
    for (let i = 1; i <= 19; i++) { persistence.save({ ...emptyPlayback(), position: i }); vi.advanceTimersByTime(100) }
    expect(writes).not.toHaveBeenCalled()
    vi.advanceTimersByTime(100)
    expect(writes).toHaveBeenCalledTimes(1)
    expect(snapshot().state.position).toBe(19)
    persistence.save({ ...emptyPlayback(), position: 20 })
    persistence.save({ ...emptyPlayback(), position: 21, playing: false }, true)
    expect(writes).toHaveBeenCalledTimes(2)
    expect(snapshot().state.position).toBe(21)
    vi.advanceTimersByTime(PLAYBACK_SAVE_INTERVAL)
    expect(writes).toHaveBeenCalledTimes(2)
    expect(snapshot()).toMatchObject({ version: 2, userId: user.id, sessionId: 'session-a', savedAt: Date.now() - PLAYBACK_SAVE_INTERVAL })
  })

  it('restores metadata across refresh and isolates users, including pending writes on switch', () => {
    vi.useFakeTimers()
    const persistence = createPlaybackPersistence(sessionStorage, () => 123, () => 'new-id')
    persistence.open(user)
    persistence.save({ ...emptyPlayback(), position: 42, recovery: { song: 1 } })
    expect(persistence.open({ id: 'bob', isDefault: false }).position).toBe(0)
    vi.advanceTimersByTime(5000)
    expect(snapshot().state.position).toBe(42)
    expect(JSON.parse(sessionStorage.getItem(playbackStorageKey('bob'))!).state.position).toBe(0)
    const refreshed = createPlaybackPersistence(sessionStorage, () => 124, () => 'different-id')
    expect(refreshed.open(user)).toMatchObject({ position: 42, recovery: { song: 1 } })
    refreshed.save({ ...emptyPlayback(), position: 43 }, true)
    expect(snapshot().sessionId).toBe('new-id')
    expect(snapshot().savedAt).toBe(124)
  })

  it.each(['broken', JSON.stringify({ version: 99 }), JSON.stringify({ version: 2, userId: 'bob', sessionId: 'foreign', savedAt: 1, state: { position: 42 } })])('rejects corrupt, unsupported or foreign snapshots: %s', (raw) => {
    sessionStorage.setItem(playbackStorageKey(user.id), raw)
    const persistence = createPlaybackPersistence(sessionStorage)
    expect(persistence.open(user)).toEqual(emptyPlayback())
    expect(snapshot().userId).toBe(user.id)
    expect(snapshot().sessionId).not.toBe('foreign')
  })

  it('writes sanitized values back during restore instead of retaining malformed random entries', () => {
    sessionStorage.setItem(playbackStorageKey(user.id), JSON.stringify({ version: 2, userId: user.id, sessionId: 'kept', savedAt: 1, state: { ...emptyPlayback(), recovery: { song: 1, bad: -1, fraction: .5 } } }))
    const persistence = createPlaybackPersistence(sessionStorage, () => 2)
    expect(persistence.open(user).recovery).toEqual({ song: 1 })
    expect(snapshot()).toMatchObject({ sessionId: 'kept', savedAt: 2, state: { recovery: { song: 1 } } })
  })

  it('migrates legacy state only for the server-confirmed default account', () => {
    const legacy = JSON.stringify({ version: 0, state: { ...emptyPlayback(), position: 12 } })
    sessionStorage.setItem('music-player-session-v1', legacy)
    expect(createPlaybackPersistence(sessionStorage).open(user).position).toBe(0)
    sessionStorage.removeItem(playbackStorageKey(user.id))
    sessionStorage.setItem('music-player-session-v1', legacy)
    expect(createPlaybackPersistence(sessionStorage).open({ ...user, isDefault: true }).position).toBe(12)
    expect(sessionStorage.getItem('music-player-session-v1')).toBeNull()
  })

  it('keeps operating if browser storage throws', () => {
    vi.useFakeTimers()
    const broken = { getItem: () => { throw Error('denied') }, setItem: () => { throw Error('quota') }, removeItem: () => { throw Error('denied') } }
    const persistence = createPlaybackPersistence(broken)
    expect(() => { persistence.open(user); persistence.save(emptyPlayback()); vi.advanceTimersByTime(2000); persistence.flush() }).not.toThrow()
  })

  it('flushes on page exit and hiding, and removes lifecycle listeners', () => {
    const flush = vi.fn()
    const stop = watchSessionLifecycle(flush)
    window.dispatchEvent(new Event('pagehide'))
    window.dispatchEvent(new Event('beforeunload'))
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(flush).toHaveBeenCalledTimes(3)
    stop(); window.dispatchEvent(new Event('pagehide'))
    expect(flush).toHaveBeenCalledTimes(3)
  })
})
