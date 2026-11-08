const storageKey = 'music-upload-session-v1'
const changeEvent = 'music-upload-session-changed'
type UploadSession = { id: string; count: number }
let fallback: UploadSession | undefined
let storageUnavailable = false

function save(session: UploadSession) {
  fallback = session
  try { window.sessionStorage.setItem(storageKey, JSON.stringify(session)) } catch { storageUnavailable = true }
}

function read(): UploadSession {
  if (storageUnavailable && fallback) return fallback
  try {
    const raw = window.sessionStorage.getItem(storageKey)
    if (raw) {
      const session = JSON.parse(raw) as UploadSession | null
      if (session && typeof session.id === 'string' && session.id && Number.isSafeInteger(session.count) && session.count >= 0) return session
    }
  } catch {
    if (fallback) return fallback
  }
  const session = { id: crypto.getRandomValues(new Uint32Array(4)).join('-'), count: 0 }
  save(session)
  return session
}

export function createUploadKey(): string {
  const previous = read()
  const session = { ...previous, count: previous.count + 1 }
  save(session)
  window.dispatchEvent(new Event(changeEvent))
  return `${session.id}:${session.count}`
}

export const uploadSession = {
  count: () => read().count,
  subscribe: (notify: () => void) => {
    window.addEventListener(changeEvent, notify)
    return () => window.removeEventListener(changeEvent, notify)
  },
}
