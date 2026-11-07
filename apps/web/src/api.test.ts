import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('playlist API normalization', () => {
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
