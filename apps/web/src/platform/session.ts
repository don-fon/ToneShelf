import type { CurrentUser, PlaybackSnapshot, PlaybackState } from '@music/domain'
import { emptyPlayback, readPlayback } from '@music/player-policy'

export interface SessionStorage {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
  removeItem: (key: string) => void
}

export const sessionStore: SessionStorage = {
  getItem: (name) => window.sessionStorage.getItem(name),
  setItem: (name, value) => window.sessionStorage.setItem(name, value),
  removeItem: (name) => window.sessionStorage.removeItem(name),
}

export const playbackStorageKey = (userId: string) => `music-player-session-v2:${encodeURIComponent(userId)}`
export const PLAYBACK_SAVE_INTERVAL = 2000

export function createPlaybackPersistence(storage: SessionStorage, now = Date.now, newID: () => string = () => crypto.randomUUID?.() ?? Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('')) {
  let userId = '', sessionId = '', pending: PlaybackState | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  const safely = <T,>(action: () => T): T | undefined => { try { return action() } catch { return undefined } }
  const flush = () => {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    if (!pending || !userId) return
    const snapshot: PlaybackSnapshot = { version: 2, userId, sessionId, savedAt: now(), state: pending }
    safely(() => storage.setItem(playbackStorageKey(userId), JSON.stringify(snapshot)))
    pending = undefined
  }
  return {
    open(user: CurrentUser): PlaybackState {
      flush()
      userId = user.id; sessionId = newID()
      const key = playbackStorageKey(userId)
      const raw = safely(() => storage.getItem(key))
      if (raw) {
        const saved = safely(() => JSON.parse(raw) as PlaybackSnapshot)
        if (saved?.version === 2 && saved.userId === userId && typeof saved.sessionId === 'string' && saved.sessionId && Number.isFinite(saved.savedAt) && saved.savedAt >= 0 && saved.state && typeof saved.state === 'object' && !Array.isArray(saved.state)) {
          sessionId = saved.sessionId
          const state = readPlayback(saved.state)
          if (JSON.stringify(state) !== JSON.stringify(saved.state)) { pending = state; flush() }
          return state
        }
        safely(() => storage.removeItem(key))
      }
      // Only the server-confirmed bootstrap account may inherit the old unscoped key.
      const legacyKey = 'music-player-session-v1'
      let state = emptyPlayback()
      if (user.isDefault) {
        const legacy = safely(() => JSON.parse(storage.getItem(legacyKey) ?? 'null'))
        if (legacy?.version === 0 && legacy.state) state = readPlayback(legacy.state)
      }
      safely(() => storage.removeItem(legacyKey))
      pending = state; flush()
      return state
    },
    save(state: PlaybackState, immediate = false) {
      if (!userId) return
      pending = state
      if (immediate) flush()
      else if (timer === undefined) timer = setTimeout(flush, PLAYBACK_SAVE_INTERVAL)
    },
    flush,
  }
}

export function watchSessionLifecycle(flush: () => void) {
  const hidden = () => { if (document.visibilityState === 'hidden') flush() }
  window.addEventListener('pagehide', flush)
  window.addEventListener('beforeunload', flush)
  document.addEventListener('visibilitychange', hidden)
  return () => {
    window.removeEventListener('pagehide', flush)
    window.removeEventListener('beforeunload', flush)
    document.removeEventListener('visibilitychange', hidden)
  }
}
