import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { App } from './App'
import { api } from './api'
import { usePlayer } from './player-store'

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
  addTrack: vi.fn().mockResolvedValue(undefined),
  moveTrack: vi.fn().mockResolvedValue(undefined),
} }))


vi.mock('./platform/audio', () => ({ WebAudioEngine: class {
  private readonly audio = document.createElement('audio')
  load() {} play() { return Promise.resolve() } pause() {} seek() {} setVolume() {} position() { return this.audio.currentTime } element() { return this.audio }
} }))

const renderApp = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><App /></QueryClientProvider>)
}

afterEach(() => { cleanup(); usePlayer.setState(usePlayer.getInitialState(), true); vi.clearAllMocks(); vi.mocked(api.addTrack).mockReset().mockResolvedValue(undefined); vi.mocked(api.moveTrack).mockReset().mockResolvedValue(undefined); vi.mocked(api.removeTrack).mockReset().mockResolvedValue({ trackDeleted: true }); vi.mocked(api.playlists).mockReset().mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [] }]) })

describe('music library lifecycle', () => {
  it('moves selected songs even when the destination already includes them', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] },
      { id: 'target', name: '目标歌单', isDefault: false, tracks: [track, second] },
    ])
    renderApp()
    const move = await screen.findByRole('button', { name: '移动到' })
    expect((move as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByLabelText('全选歌曲'))
    fireEvent.click(move)
    const dialog = screen.getByRole('dialog', { name: '移动到歌单' })
    const target = within(dialog).getByRole('button', { name: /目标歌单/ }) as HTMLButtonElement
    expect(target.disabled).toBe(false)
    fireEvent.click(target)
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(vi.mocked(api.moveTrack).mock.calls).toEqual([['default', 'target', track.id], ['default', 'target', 'second']])
    expect(api.addTrack).not.toHaveBeenCalled()
    expect(api.removeTrack).not.toHaveBeenCalled()
    expect(screen.getByRole('status').textContent).toContain('已移动 2 首')
  })

  it('keeps only failed moves selected and retries through the atomic move endpoint', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    const target = { id: 'target', name: '目标歌单', isDefault: false, tracks: [] as Track[] }
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] }, target]).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [second] }, { ...target, tracks: [track] }])
    vi.mocked(api.moveTrack).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('move failed'))
    renderApp()
    fireEvent.click(await screen.findByLabelText('全选歌曲'))
    fireEvent.click(screen.getByRole('button', { name: '移动到' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /目标歌单/ }))
    expect((await screen.findByRole('alert')).textContent).toContain('已移动 1 首，1 首失败')
    expect((screen.getByLabelText('选择 Second') as HTMLInputElement).checked).toBe(true)
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /目标歌单/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(vi.mocked(api.moveTrack).mock.calls).toEqual([['default', 'target', track.id], ['default', 'target', 'second'], ['default', 'target', 'second']])
  })

  it('adds selected songs to another playlist and skips songs already present', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] },
      { id: 'target', name: '目标歌单', isDefault: false, tracks: [track] },
      { id: 'included', name: '已有歌单', isDefault: false, tracks: [track, second] },
    ])
    renderApp()
    const add = await screen.findByRole('button', { name: '添加到' })
    expect((add as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByText('从音乐库添加歌曲')).toBeNull()
    fireEvent.click(screen.getByLabelText('全选歌曲'))
    fireEvent.click(add)
    expect(screen.getByRole('dialog', { name: '添加到歌单' }).textContent).toContain('已选择 2 首')
    expect((within(screen.getByRole('dialog')).getByRole('button', { name: /已有歌单/ }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /目标歌单/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(api.addTrack).toHaveBeenCalledExactlyOnceWith('target', 'second')
    expect(api.removeTrack).not.toHaveBeenCalled()
    expect(screen.getByRole('status').textContent).toContain('已添加 1 首到“目标歌单”')
  })

  it('adds a song back to the default playlist from an ordinary playlist', async () => {
    usePlayer.setState({ playlistID: 'source' })
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'source', name: '普通歌单', isDefault: false, tracks: [track] },
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [] },
    ])
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button', { name: '添加到' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /默认歌单/ }))
    await waitFor(() => expect(api.addTrack).toHaveBeenCalledExactlyOnceWith('default', track.id))
    expect(api.removeTrack).not.toHaveBeenCalled()
  })

  it('keeps failed selections for retry after a partial batch failure', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    const source = { id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] }
    const target = { id: 'target', name: '目标歌单', isDefault: false, tracks: [] as Track[] }
    vi.mocked(api.playlists).mockResolvedValueOnce([source, target]).mockResolvedValueOnce([source, { ...target, tracks: [track] }])
    vi.mocked(api.addTrack).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('failed'))
    renderApp()
    fireEvent.click(await screen.findByLabelText('全选歌曲'))
    fireEvent.click(screen.getByRole('button', { name: '添加到' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /目标歌单/ }))
    expect((await screen.findByRole('alert')).textContent).toContain('已添加 1 首，1 首失败')
    expect((screen.getByLabelText('选择 Second') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('选择 World is Mine') as HTMLInputElement).checked).toBe(false)
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /目标歌单/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(vi.mocked(api.addTrack).mock.calls).toEqual([['target', track.id], ['target', 'second'], ['target', 'second']])
  })

  it('explains when no other playlist exists and allows cancelling', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button', { name: '添加到' }))
    expect(screen.getByText('请先在侧栏创建另一个歌单。')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect((screen.getByLabelText('选择 World is Mine') as HTMLInputElement).checked).toBe(true)
    expect(api.addTrack).not.toHaveBeenCalled()
  })

  it('hides same-title album repetition while keeping distinct album information', async () => {
    const repeated = { ...track, id: 'repeated', title: '嘘の火花', artist: '96猫', album: ' 嘘の火花 ' }
    const distinct = { ...track, id: 'distinct', title: 'あなたに出会わなければ', artist: 'Aimer', album: 'Sleepless Nights' }
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [repeated, distinct] }])
    renderApp()
    const first = await screen.findByText(repeated.title)
    expect(first.closest('.track')?.querySelector('small')?.textContent).toBe('96猫')
    expect(screen.getAllByText(repeated.title)).toHaveLength(1)
    expect(screen.getByText(distinct.title).closest('.track')?.querySelector('small')?.textContent).toBe('Aimer · Sleepless Nights')
  })

  it('previews a manually edited offset and saves the raw lyrics without an offset control', async () => {
    const lyrics = '[offset:0]\n[00:01]First\n[00:04]Second'
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [{ ...track, lyrics }] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    fireEvent.click(screen.getByText('歌词显示'))
    expect(screen.queryByLabelText('整体偏移（秒）')).toBeNull()
    const edited = lyrics.replace('[offset:0]', '[offset:-1250]')
    fireEvent.change(screen.getByLabelText('歌词'), { target: { value: edited } })
    expect(screen.getByRole('region', { name: '歌词显示预览' }).textContent).toContain('0:02.250')
    expect(api.updateTrack).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(vi.mocked(api.updateTrack).mock.calls[0]?.[0]).toEqual(expect.objectContaining({ lyrics: edited })))
  })

  it('saves per-song lyrics interpretation only after the user confirms', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    expect(screen.queryByText('手动修正时间')).toBeNull()
    fireEvent.click(screen.getByText('歌词显示'))
    fireEvent.change(screen.getByLabelText('分组方式'), { target: { value: 'timestamp' } })
    fireEvent.change(screen.getByLabelText('主行'), { target: { value: 'last' } })
    expect(api.updateTrack).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(vi.mocked(api.updateTrack).mock.calls[0]?.[0]).toEqual(expect.objectContaining({ id: track.id, lyricsLayout: 'timestamp', lyricsPrimaryLine: 'last' })))
  })

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
    const remove = await screen.findByRole('button', { name: '删除' })
    expect((remove as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByLabelText('从歌单移除 World is Mine')).toBeNull()
    fireEvent.click(screen.getByLabelText('选择 World is Mine'))
    fireEvent.click(remove)
    await waitFor(() => expect(api.removeTrack).toHaveBeenCalledWith('default', 'track-1', true))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('将进入回收站'))
  })

  it('confirms a mixed batch once and only authorizes deletion of default-only songs', async () => {
    const shared = { ...track, id: 'shared', title: 'Shared' }
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [track, shared] },
      { id: 'other', name: '其他歌单', isDefault: false, tracks: [shared] },
    ])
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    fireEvent.click(await screen.findByLabelText('全选歌曲'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(api.removeTrack).toHaveBeenCalledTimes(2))
    expect(api.removeTrack).toHaveBeenCalledWith('default', track.id, true)
    expect(api.removeTrack).toHaveBeenCalledWith('default', 'shared', false)
    expect(window.confirm).toHaveBeenCalledTimes(1)
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('其中 1 首'))
  })

  it('removes from an ordinary playlist without authorizing global deletion', async () => {
    usePlayer.setState({ playlistID: 'ordinary' })
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'ordinary', name: '普通歌单', isDefault: false, tracks: [track] },
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [] },
    ])
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(api.removeTrack).toHaveBeenCalledExactlyOnceWith('ordinary', track.id, false))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('音乐文件会保留'))
  })

  it('keeps the selection and sends no requests when deletion is cancelled', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect(api.removeTrack).not.toHaveBeenCalled()
    expect((screen.getByLabelText('选择 World is Mine') as HTMLInputElement).checked).toBe(true)
  })

  it('keeps failed deletion selections for retry without repeating successful requests', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] }]).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [second] }])
    vi.mocked(api.removeTrack).mockResolvedValueOnce({ trackDeleted: true }).mockRejectedValueOnce(new Error('failed'))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    fireEvent.click(await screen.findByLabelText('全选歌曲'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect((await screen.findByRole('alert')).textContent).toContain('已删除 1 首，1 首失败')
    expect((screen.getByLabelText('选择 Second') as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(vi.mocked(api.removeTrack).mock.calls).toEqual([['default', track.id, true], ['default', 'second', true], ['default', 'second', true]]))
  })

  it('edits title and artist together with song settings', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    expect(screen.queryByRole('button', { name: '移入回收站' })).toBeNull()
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
