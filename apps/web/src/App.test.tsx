import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { App } from './App'
import { api } from './api'

const track: Track = {
  id: 'track-1', title: 'World is Mine', artist: 'ryo', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
  durationSeconds: 255.24, baseRandomWeight: 1, playbackStartSeconds: 0, playbackEndSeconds: null,
  volumeRatio: 0.8, lyrics: '', audioUrl: '/audio/track-1?v=1', audioVersion: 1, fileName: 'World is Mine.mp3', uploadedAt: '2026-11-08T00:00:00Z',
}

vi.mock('./api', () => ({ api: {
  jobs: vi.fn().mockResolvedValue([]),
  playlists: vi.fn().mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [] }]),
  quota: vi.fn().mockResolvedValue({ usedBytes: 0, limitBytes: 10 * 1024 ** 3, remainingBytes: 10 * 1024 ** 3 }),
  trash: vi.fn().mockResolvedValue([]),
  searchLyrics: vi.fn().mockResolvedValue({ queryTitle: 'World is Mine', queryArtist: 'ryo', candidates: [
    { id: 1, trackName: 'World is Mine', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
    { id: 2, trackName: 'World is Mine', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
  ] }),
  removeTrack: vi.fn().mockResolvedValue({ trackDeleted: true }),
  updateTrack: vi.fn().mockResolvedValue({}),
  restoreTrack: vi.fn().mockResolvedValue({}),
} }))

vi.mock('./player-store', () => ({ usePlayer: () => ({
  playlistID: null, trackID: null, audioVersion: null, mode: 'random', playing: false, masterVolume: 1, position: 0, recovery: {},
  selectPlaylist: vi.fn(), selectTrack: vi.fn(), setAudioVersion: vi.fn(), setMode: vi.fn(), setPlaying: vi.fn(), setMasterVolume: vi.fn(), setPosition: vi.fn(),
}) }))

vi.mock('./platform/audio', () => ({ WebAudioEngine: class {
  private readonly audio = document.createElement('audio')
  load() {} play() { return Promise.resolve() } pause() {} seek() {} setVolume() {} element() { return this.audio }
} }))

const renderApp = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><App /></QueryClientProvider>)
}

afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('music library lifecycle', () => {
  it('renders an empty library and the trash entry', async () => {
    renderApp()
    expect(await screen.findByText('这里还没有音乐')).toBeTruthy()
    expect(screen.getByText('回收站')).toBeTruthy()
    expect(document.querySelector('input[type="file"]')?.getAttribute('accept')).toContain('.aac')
  })

  it('lets the user choose one lyrics candidate', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    expect((screen.getByLabelText('随机权重') as HTMLInputElement).step).toBe('any')
    fireEvent.click(screen.getByText('从 LRCLIB 匹配'))
    await screen.findAllByText('World is Mine')
    fireEvent.click(document.querySelectorAll<HTMLButtonElement>('.lyrics-results button')[0])
    expect((screen.getByLabelText('歌词') as HTMLTextAreaElement).value).toBe('[00:01.00] synced')
    expect(document.querySelectorAll('.lyrics-results button.selected').length).toBe(1)
  })

  it('confirms before removing a default-only track', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    fireEvent.click(await screen.findByLabelText('从歌单移除 World is Mine'))
    await waitFor(() => expect(api.removeTrack).toHaveBeenCalledWith('default', 'track-1', true))
  })

  it('edits title and artist together with song settings', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    fireEvent.change(screen.getByLabelText('歌曲名称'), { target: { value: '新名称' } })
    fireEvent.change(screen.getByLabelText('歌手'), { target: { value: '新歌手' } })
    fireEvent.click(screen.getByText('保存'))
    await waitFor(() => expect(api.updateTrack).toHaveBeenCalledWith(expect.objectContaining({ title: '新名称', artist: '新歌手' }), expect.anything()))
  })

  it('switches displayed filename order without manual move controls', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [
      { ...track, id: 'ten', title: 'Ten', fileName: '10.mp3', uploadedAt: '2026-11-09T00:00:00Z' },
      { ...track, id: 'two', title: 'Two', fileName: '2.mp3', uploadedAt: '2026-11-08T00:00:00Z' },
    ] }])
    renderApp()
    await screen.findByText('Ten')
    const titles = () => Array.from(document.querySelectorAll('.track strong')).map((element) => element.textContent)
    expect(titles()).toEqual(['Ten', 'Two'])
    expect(screen.queryByRole('group', { name: '排序选项' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '排序' }))
    fireEvent.click(screen.getByRole('button', { name: '文件名 · 正序' }))
    expect(screen.queryByRole('group', { name: '排序选项' })).toBeNull()
    expect(titles()).toEqual(['Two', 'Ten'])
    fireEvent.click(screen.getByRole('button', { name: '排序' }))
    fireEvent.click(screen.getByRole('button', { name: '文件名 · 逆序' }))
    expect(titles()).toEqual(['Ten', 'Two'])
    expect(screen.queryByLabelText('上移 Ten')).toBeNull()
  })

  it('shows trashed tracks and restores one', async () => {
    vi.mocked(api.trash).mockResolvedValueOnce([{ ...track, deletedAt: '2026-11-08T00:00:00Z', purgeEligibleAt: '2026-11-11T00:00:00Z' }])
    renderApp()
    fireEvent.click(await screen.findByText('回收站'))
    fireEvent.click(await screen.findByText('恢复'))
    await waitFor(() => expect(api.restoreTrack).toHaveBeenCalledWith('track-1'))
  })
})
