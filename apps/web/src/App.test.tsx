import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'

vi.mock('./api', () => ({
  api: {
    playlists: vi.fn().mockResolvedValue([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [] },
    ]),
    quota: vi.fn().mockResolvedValue({ usedBytes: 0, limitBytes: 10 * 1024 ** 3, remainingBytes: 10 * 1024 ** 3 }),
  },
}))

vi.mock('./player-store', () => ({
  usePlayer: () => ({
    playlistID: null,
    trackID: null,
    mode: 'random',
    playing: false,
    masterVolume: 1,
    position: 0,
    recovery: {},
    selectPlaylist: vi.fn(),
    selectTrack: vi.fn(),
    setMode: vi.fn(),
    setPlaying: vi.fn(),
    setMasterVolume: vi.fn(),
    setPosition: vi.fn(),
  }),
}))

vi.mock('./platform/audio', () => ({
  WebAudioEngine: class {
    private readonly audio = document.createElement('audio')
    load() {}
    play() { return Promise.resolve() }
    pause() {}
    seek() {}
    setVolume() {}
    element() { return this.audio }
  },
}))

afterEach(() => {
  cleanup()
})

describe('empty music library', () => {
  it('renders without crashing', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><App /></QueryClientProvider>)

    expect(await screen.findByText('这里还没有音乐')).toBeTruthy()
    expect(screen.getAllByText('默认歌单').length).toBe(2)
    expect(document.querySelector('input[type="file"]')?.getAttribute('accept')).toContain('.aac')
  })
})
