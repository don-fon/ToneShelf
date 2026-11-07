import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    playlists: vi.fn().mockResolvedValue([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [] },
    ]),
    quota: vi.fn().mockResolvedValue({ usedBytes: 0, limitBytes: 10 * 1024 ** 3, remainingBytes: 10 * 1024 ** 3 }),
    searchLyrics: vi.fn().mockResolvedValue({
      queryTitle: 'ワールドイズマイン', queryArtist: 'ryo', candidates: [
        { id: 29152005, trackName: 'ワールドイズマイン [World Is Mine]', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
        { id: 29152006, trackName: 'ワールドイズマイン [World Is Mine]', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
      ],
    }),
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
  vi.clearAllMocks()
})

describe('empty music library', () => {
  it('renders without crashing', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><App /></QueryClientProvider>)

    expect(await screen.findByText('这里还没有音乐')).toBeTruthy()
    expect(screen.getAllByText('默认歌单').length).toBe(2)
    expect(document.querySelector('input[type="file"]')?.getAttribute('accept')).toContain('.aac')
  })

  it('lets the user choose a lyrics candidate before saving', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [{
      id: 'track-1', title: 'ryo _ 初音未来 - ワールドイズマイン (World is Mine)_H', artist: '', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
      durationSeconds: 255.24, baseRandomWeight: 1, playbackStartSeconds: 0, playbackEndSeconds: null, volumeRatio: 0.8, lyrics: '', audioUrl: '/audio/track-1',
    }] }])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><App /></QueryClientProvider>)

    fireEvent.click(await screen.findByText('设置'))
    const weight = screen.getByLabelText('随机权重') as HTMLInputElement
    expect(weight.step).toBe('any')
    expect(weight.checkValidity()).toBe(true)
    fireEvent.click(screen.getByText('从 LRCLIB 匹配'))
    fireEvent.click((await screen.findAllByText('ワールドイズマイン [World Is Mine]'))[0])

    expect((screen.getByLabelText('歌词') as HTMLTextAreaElement).value).toBe('[00:01.00] synced')
    expect(document.querySelectorAll('.lyrics-results button.selected').length).toBe(1)
  })
})
