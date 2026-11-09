import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { uploadSession } from './platform/upload-session'
import { uploadFile } from './platform/upload'

vi.mock('./platform/upload', () => ({ uploadFile: vi.fn() }))

afterEach(() => { vi.unstubAllGlobals(); sessionStorage.clear() })

describe('playlist API normalization', () => {
  it('moves a song through one request containing the destination playlist', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetch)
    await api.moveTrack('source', 'target', 'song')
    expect(fetch).toHaveBeenCalledExactlyOnceWith('/api/playlists/source/tracks/song/move', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ targetPlaylistId: 'target' }) })
  })
  it('passes the selected playlist to upload precheck with URL encoding', () => {
    const file = new File(['audio'], 'new.mp3')
    const progress = vi.fn()
    api.prepareUpload(file, progress, 'A & B')
    expect(uploadFile).toHaveBeenCalledWith('/api/uploads/check?playlistId=A%20%26%20B', file, progress)
  })
  it('counts an audio replacement once and attaches its session request key', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ id: 'replacement' }) })
    vi.stubGlobal('fetch', fetch)
    expect(uploadSession.count()).toBe(0)
    await api.replaceAudio('song', new File(['audio'], 'replacement.mp3'))
    expect(uploadSession.count()).toBe(1)
    expect(fetch.mock.calls[0][0]).toBe('/api/tracks/song/audio')
    expect(fetch.mock.calls[0][1].headers['X-Upload-Key']).toMatch(/:1$/)
  })
  it('normalizes a null playlist collection', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => null,
    }))

    await expect(api.playlists()).resolves.toEqual([])
  })

  it('normalizes null tracks in a playlist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => [{ id: 'default', name: '默认歌单', isDefault: true, tracks: null }],
    }))

    await expect(api.playlists()).resolves.toEqual([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [] },
    ])
  })
})
