import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => { vi.restoreAllMocks(); sessionStorage.clear(); vi.resetModules() })

describe('upload session display count', () => {
  it('restores the count and continues unique request keys after a reload', async () => {
    const first = await import('./upload-session')
    expect(first.uploadSession.count()).toBe(0)
    const key = first.createUploadKey()
    expect(first.uploadSession.count()).toBe(1)
    vi.resetModules()
    const restored = await import('./upload-session')
    expect(restored.uploadSession.count()).toBe(1)
    const next = restored.createUploadKey()
    expect(next).toBe(key.replace(/:1$/, ':2'))
    expect(restored.uploadSession.count()).toBe(2)
  })

  it('keeps counting in memory when session storage rejects writes', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable') })
    const session = await import('./upload-session')
    expect(session.uploadSession.count()).toBe(0)
    const first = session.createUploadKey()
    const second = session.createUploadKey()
    expect(first).not.toBe(second)
    expect(session.uploadSession.count()).toBe(2)
  })

  it('resets invalid saved display state without blocking uploads', async () => {
    sessionStorage.setItem('music-upload-session-v1', '{invalid')
    const session = await import('./upload-session')
    expect(session.uploadSession.count()).toBe(0)
    expect(session.createUploadKey()).toMatch(/:1$/)
    expect(session.uploadSession.count()).toBe(1)
  })
})
