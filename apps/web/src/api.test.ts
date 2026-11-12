import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import type { Track } from '@music/domain'
import { uploadSession } from './platform/upload-session'
import { uploadFile } from './platform/upload'
import { advanceAuthGeneration, watchAuthExpired } from './platform/auth-events'

vi.mock('./platform/upload', () => ({ uploadFile: vi.fn() }))

afterEach(() => { vi.unstubAllGlobals(); sessionStorage.clear() })

it('does not send tag associations when saving song playback and lyrics settings', async () => {
  const fetch=vi.fn().mockResolvedValue({ok:true,status:200,json:async()=>({})})
  vi.stubGlobal('fetch',fetch)
  await api.updateTrack({id:'song',title:'Song',artist:'',tagIds:['old-tag']} as Track)
  expect(JSON.parse(fetch.mock.calls[0][1].body)).not.toHaveProperty('tagIds')
})

it('does not let a delayed unauthorized response clear a newer login session', async () => {
  let finish!: (value: unknown) => void
  vi.stubGlobal('fetch', vi.fn(() => new Promise((resolve) => { finish = resolve })))
  const expired = vi.fn(); const stop = watchAuthExpired(expired)
  const pending = api.playlists()
  advanceAuthGeneration()
  finish({ ok: false, status: 401, json: async () => ({ error: '请先登录' }) })
  await expect(pending).rejects.toMatchObject({ status: 401 })
  expect(expired).not.toHaveBeenCalled()
  stop()
})

describe('playlist API normalization', () => {
  it('sends song notes including an explicit empty value', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    vi.stubGlobal('fetch', fetch)
    for (const note of ['第一行\n第二行 🎵', '']) {
      await api.updateTrack({ id: 'song', note } as Track)
      expect(JSON.parse(fetch.mock.calls.at(-1)![1].body)).toMatchObject({ note })
    }
  })
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
