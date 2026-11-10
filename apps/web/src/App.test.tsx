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
  replaceCover: vi.fn(),
  replaceAudio: vi.fn(),
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
  it.each(['.cover', '.now-meta strong', '.now-meta small'])('opens lyrics from the player information area %s', async (selector) => {
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('group', { name: '歌曲 World is Mine' }))
    const entry = screen.getByRole('button', { name: '查看歌词：World is Mine' })
    expect(entry.contains(document.querySelector('.player .cover'))).toBe(true)
    expect(entry.contains(document.querySelector('.player small'))).toBe(true)
    fireEvent.click(entry.querySelector(selector)!)
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('lyrics')
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
  })
  it('groups trash and storage apart from playlists and keeps active uploads near the upload button', async () => {
    vi.mocked(api.jobs).mockResolvedValueOnce([{ id: 'upload', state: 'processing', kind: 'upload', fileName: 'test.mp3', createdAt: '', reservedBytes: 0 }])
    renderApp()
    const tools = await screen.findByRole('group', { name: '音乐库工具' })
    const trash = within(tools).getByRole('button', { name: '回收站' })
    expect(within(tools).getByRole('button', { name: '存储空间' })).toBeTruthy()
    expect(await screen.findByText('上传中')).toBeTruthy()
    expect(tools.querySelector('.upload-tasks')).toBeNull()
    expect(document.querySelector('.upload-actions .upload-tasks')).toBeTruthy()
    expect(within(screen.getByRole('navigation')).queryByRole('button', { name: '回收站' })).toBeNull()
    expect(screen.queryByText('音乐空间')).toBeNull()
    expect(screen.queryByText('你的私人播放站')).toBeNull()
    fireEvent.click(trash)
    expect(trash.getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('heading', { name: '回收站' })).toBeTruthy()
  })
  it('opens lyrics from the player title and returns through the current playlist without resetting playback', async () => {
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('group', { name: '歌曲 World is Mine' }))
    expect(screen.queryByRole('group', { name: '播放视图' })).toBeNull()
    const recovery = usePlayer.getState().recovery
    usePlayer.setState({ position: 42 })
    fireEvent.click(screen.getByRole('button', { name: '查看歌词：World is Mine' }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('lyrics')
    expect(usePlayer.getState().playing).toBe(true)
    fireEvent.click(within(screen.getByRole('navigation')).getByRole('button', { name: /默认歌单/ }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('songs')
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().position).toBe(42)
    expect(usePlayer.getState().recovery).toEqual(recovery)
  })

  it('opens the selected playlist from lyrics and opens lyrics from the trash view', async () => {
    vi.mocked(api.playlists).mockResolvedValue([
      { id: 'default', name: '默认歌单', isDefault: true, tracks: [track] },
      { id: 'other', name: '其他歌单', isDefault: false, tracks: [track] },
    ])
    renderApp()
    fireEvent.click(await screen.findByRole('group', { name: '歌曲 World is Mine' }))
    fireEvent.click(screen.getByRole('button', { name: '查看歌词：World is Mine' }))
    fireEvent.click(within(screen.getByRole('navigation')).getByRole('button', { name: /其他歌单/ }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('songs')
    expect(screen.getByRole('heading', { name: '其他歌单' })).toBeTruthy()
    fireEvent.click(screen.getByRole('group', { name: '歌曲 World is Mine' }))
    fireEvent.click(screen.getByRole('button', { name: '回收站' }))
    fireEvent.click(screen.getByRole('button', { name: '查看歌词：World is Mine' }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('lyrics')
    expect(screen.getByRole('region', { name: '播放歌词' })).toBeTruthy()
  })

  it('plays by clicking the song row and resumes the current song without resetting progress', async () => {
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    const row = await screen.findByRole('group', { name: '歌曲 World is Mine' })
    expect(row.querySelector('button.play')).toBeNull()
    fireEvent.click(within(row).getByText('World is Mine'))
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
    usePlayer.setState({ position: 42, playing: false })
    fireEvent.click(row)
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().position).toBe(42)
    const recovery = usePlayer.getState().recovery
    fireEvent.click(row)
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().position).toBe(42)
    expect(usePlayer.getState().recovery).toEqual(recovery)
  })

  it('keeps checkbox and settings interactions separate from row playback', async () => {
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    const row = await screen.findByRole('group', { name: '歌曲 World is Mine' })
    fireEvent.click(within(row).getByRole('checkbox'))
    expect(usePlayer.getState().trackID).toBeNull()
    fireEvent.keyDown(within(row).getByRole('checkbox'), { key: ' ' })
    expect(usePlayer.getState().playing).toBe(false)
    const settingsButton = within(row).getByRole('button', { name: '设置 World is Mine' })
    fireEvent.click(settingsButton.querySelector('svg')!)
    expect(screen.getByRole('dialog', { name: '歌曲设置' })).toBeTruthy()
    expect(usePlayer.getState().playing).toBe(false)
  })

  it.each(['Enter', ' '])('plays the focused song row with %s', async (key) => {
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    const row = await screen.findByRole('group', { name: '歌曲 World is Mine' })
    fireEvent.keyDown(row, { key })
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
  })

  it('discards selected cover and audio files on cancel without uploading either', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:preview'); URL.revokeObjectURL = vi.fn()
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    fireEvent.change(screen.getByLabelText('选择封面图片'), { target: { files: [new File(['image'], 'cover.png')] } })
    fireEvent.change(screen.getByLabelText('替换音频文件'), { target: { files: [new File(['audio'], 'new.mp3')] } })
    await screen.findByAltText('新封面预览')
    expect(screen.getByText('待替换音频：new.mp3')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(api.updateTrack).not.toHaveBeenCalled()
    expect(api.replaceCover).not.toHaveBeenCalled()
    expect(api.replaceAudio).not.toHaveBeenCalled()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview')
    fireEvent.click(screen.getByRole('button', { name: '设置 World is Mine' }))
    expect(screen.queryByAltText('新封面预览')).toBeNull()
    expect(screen.queryByText('待替换音频：new.mp3')).toBeNull()
  })

  it('submits the selected cover through the main save button and waits for processing', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:preview'); URL.revokeObjectURL = vi.fn()
    vi.mocked(api.playlists).mockResolvedValue([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    let complete!: (job: Awaited<ReturnType<typeof api.replaceCover>>) => void
    vi.mocked(api.replaceCover).mockImplementationOnce(() => new Promise((resolve) => { complete = resolve }))
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    const file = new File(['image'], 'cover.png')
    fireEvent.change(screen.getByLabelText('选择封面图片'), { target: { files: [file] } })
    fireEvent.change(screen.getByLabelText('歌曲名称'), { target: { value: 'Edited title' } })
    expect(api.replaceCover).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '保存封面' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(api.replaceCover).toHaveBeenCalledExactlyOnceWith(track.id, file))
    expect(vi.mocked(api.updateTrack).mock.calls[0][0].title).toBe('Edited title')
    expect((screen.getByRole('button', { name: '保存中…' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: '取消' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByLabelText('歌曲名称') as HTMLInputElement).matches(':disabled')).toBe(true)
    complete({ id: 'cover-job', kind: 'cover', state: 'succeeded', fileName: '', createdAt: '', reservedBytes: 0 })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '歌曲设置' })).toBeNull())
  })

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
    expect(screen.queryByLabelText('整体偏移（秒）')).toBeNull()
    const edited = lyrics.replace('[offset:0]', '[offset:-1250]')
    fireEvent.change(screen.getByLabelText('歌词'), { target: { value: edited } })
    fireEvent.click(screen.getByRole('tab', { name: '显示设置' }))
    expect(screen.getByRole('region', { name: '歌词显示预览' }).textContent).toContain('0:02.250')
    fireEvent.click(screen.getByRole('tab', { name: '编辑' }))
    expect(screen.getByRole('textbox', { name: '歌词' })).toHaveProperty('value', edited)
    expect(api.updateTrack).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(vi.mocked(api.updateTrack).mock.calls[0]?.[0]).toEqual(expect.objectContaining({ lyrics: edited })))
  })

  it('saves per-song lyrics interpretation only after the user confirms', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    expect(screen.queryByText('手动修正时间')).toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: '显示设置' }))
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
    await waitFor(() => expect(api.updateTrack).toHaveBeenCalledWith(expect.objectContaining({ title: '新名称', artist: '新歌手' })))
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
