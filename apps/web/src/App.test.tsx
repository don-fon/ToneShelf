import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Track, Playlist, Library } from '@music/domain'
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
  library: vi.fn().mockResolvedValue({ tracks: [], tags: [], groups: [{ id: 'default', name: '默认标签组', isDefault: true, tagIds: ['_untagged'] }] }),
  quota: vi.fn().mockResolvedValue({ usedBytes: 0, limitBytes: 10 * 1024 ** 3, remainingBytes: 10 * 1024 ** 3 }),
  trash: vi.fn().mockResolvedValue([]),
  searchLyrics: vi.fn().mockResolvedValue({ queryTitle: 'World is Mine', queryArtist: 'ryo', candidates: [
    { id: 1, trackName: 'World is Mine', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
    { id: 2, trackName: 'World is Mine', artistName: 'ryo', albumName: 'supercell', duration: 253, synced: true, lyrics: '[00:01.00] synced', score: 95 },
  ] }),
  deleteTrack: vi.fn().mockResolvedValue(undefined),
  saveTag: vi.fn().mockResolvedValue(undefined),
  setTrackTags: vi.fn().mockResolvedValue(undefined),
  updateTrack: vi.fn().mockResolvedValue({}),
  replaceCover: vi.fn(),
  replaceAudio: vi.fn(),
  restoreTrack: vi.fn().mockResolvedValue({}),
  addTrack: vi.fn().mockResolvedValue(undefined),
  moveTrack: vi.fn().mockResolvedValue(undefined),
} }))


vi.mock('./platform/audio', () => ({ WebAudioEngine: class {
  private readonly audio = document.createElement('audio')
  load() {} play() { return Promise.resolve() } pause() {} unload() {} seek() {} setVolume() {} position() { return this.audio.currentTime } element() { return this.audio }
} }))


function asLibrary(playlists: Playlist[]): Library {
  const tracks = [...new Map(playlists.flatMap((p) => p.tracks).map((track) => [track.id, track])).values()]
  return { tracks: tracks.map((track) => ({ ...track, tagIds: [] })), tags: [], groups: [{ id: 'default', name: '默认标签组', isDefault: true, tagIds: ['_untagged'] }] }
}
const mockLibrary = (playlists: Playlist[]) => vi.mocked(api.library).mockResolvedValue(asLibrary(playlists))
const mockLibraryOnce = (playlists: Playlist[]) => vi.mocked(api.library).mockResolvedValueOnce(asLibrary(playlists))

const renderApp = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><App /></QueryClientProvider>)
}

afterEach(() => { cleanup(); vi.unstubAllGlobals(); usePlayer.setState(usePlayer.getInitialState(), true); vi.clearAllMocks(); vi.mocked(api.addTrack).mockReset().mockResolvedValue(undefined); vi.mocked(api.moveTrack).mockReset().mockResolvedValue(undefined); vi.mocked(api.deleteTrack).mockReset().mockResolvedValue(undefined); vi.mocked(api.library).mockReset().mockResolvedValue(asLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [] }])) })

describe('music library lifecycle', () => {
  it('edits multiline song notes, discards cancelled drafts and supports clearing', async () => {
    mockLibrary([{id:'default',name:'',isDefault:true,tracks:[{...track,note:'原备注'}]}])
    renderApp()
    fireEvent.click(await screen.findByRole('button',{name:'设置 World is Mine'}))
    fireEvent.change(screen.getByLabelText('备注'),{target:{value:'未保存'}})
    fireEvent.click(screen.getByRole('button',{name:'取消'}))
    expect(api.updateTrack).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button',{name:'设置 World is Mine'}))
    expect((screen.getByLabelText('备注') as HTMLTextAreaElement).value).toBe('原备注')
    fireEvent.change(screen.getByLabelText('备注'),{target:{value:'第一行\n第二行 🎵'}})
    fireEvent.click(screen.getByRole('button',{name:'保存'}))
    await waitFor(()=>expect(api.updateTrack).toHaveBeenLastCalledWith(expect.objectContaining({note:'第一行\n第二行 🎵'})))
    await waitFor(()=>expect(screen.queryByRole('dialog',{name:'歌曲设置'})).toBeNull())
    fireEvent.click(screen.getByRole('button',{name:'设置 World is Mine'}))
    fireEvent.change(screen.getByLabelText('备注'),{target:{value:''}})
    fireEvent.click(screen.getByRole('button',{name:'保存'}))
    await waitFor(()=>expect(api.updateTrack).toHaveBeenLastCalledWith(expect.objectContaining({note:''})))
  })
  it('uses the top tag area for narrow-screen bulk actions and restores it after clearing selection', async () => {
    vi.stubGlobal('matchMedia',vi.fn(() => ({matches:true,addEventListener:vi.fn(),removeEventListener:vi.fn()})))
    mockLibrary([{id:'default',name:'',isDefault:true,tracks:[track,{...track,id:'second',title:'Second'}]}])
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    const button=screen.getByRole('button',{name:'设置标签'})
    expect(document.querySelector('.mobile-selection-slot')?.contains(button)).toBe(true)
    expect(document.querySelector('.playlist-toolbar')?.contains(button)).toBe(false)
    expect(document.querySelector('.tag-panel')?.getAttribute('data-selecting')).toBe('true')
    expect(usePlayer.getState().tagSelection).toBeUndefined()
    fireEvent.click(screen.getByRole('button',{name:'全选'}))
    expect((screen.getByLabelText('选择 Second') as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole('button',{name:'取消全选'}))
    expect(screen.queryByRole('button',{name:'设置标签'})).toBeNull()
    expect(document.querySelector('.tag-panel')?.getAttribute('data-selecting')).toBe('false')
  })
  it('keeps selected songs and the dialog open when creating a tag in the default all-tags view', async () => {
    const initial=asLibrary([{id:'default',name:'',isDefault:true,tracks:[track]}])
    const created={id:'new',name:'新'}
    const updated={...initial,tags:[created]}
    vi.mocked(api.library).mockResolvedValueOnce(initial).mockResolvedValueOnce(initial).mockResolvedValue(updated)
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button',{name:'设置标签'}))
    const dialog=screen.getByRole('dialog',{name:'设置歌曲标签'})
    fireEvent.change(within(dialog).getByLabelText('新建标签'),{target:{value:'新'}})
    fireEvent.click(within(dialog).getByRole('button',{name:'创建并添加'}))
    await waitFor(() => expect(api.saveTag).toHaveBeenCalledWith('新'))
    await waitFor(() => expect(within(dialog).getByRole('button',{name:'标签 新'}).getAttribute('aria-pressed')).toBe('true'))
    expect(screen.getByRole('dialog',{name:'设置歌曲标签'})).toBe(dialog)
    expect(within(dialog).getByText('已选择 1 首歌曲')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button',{name:'保存'}))
    await waitFor(() => expect(api.setTrackTags).toHaveBeenCalledWith([track.id],['new'],'add'))
  })

  it('shows small song labels in the list and keeps tag controls out of song settings', async () => {
    vi.mocked(api.library).mockResolvedValue({...asLibrary([{id:'default',name:'',isDefault:true,tracks:[track]}]),tracks:[{...track,tagIds:['jp']}],tags:[{id:'jp',name:'日语'}]})
    renderApp()
    const row=await screen.findByRole('group',{name:'歌曲 World is Mine'})
    expect(within(row).getByLabelText('歌曲标签：日语')).toBeTruthy()
    fireEvent.click(screen.getByRole('button',{name:'设置 World is Mine'}))
    expect(within(screen.getByRole('dialog',{name:'歌曲设置'})).queryByRole('button',{name:'标签 日语'})).toBeNull()
  })
  it.each(['.cover', '.now-meta strong', '.now-meta small'])('opens lyrics from the player information area %s', async (selector) => {
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('group', { name: '歌曲 World is Mine' }))
    const entry = screen.getByRole('button', { name: '查看歌词：World is Mine' })
    expect(entry.contains(document.querySelector('.player .cover'))).toBe(true)
    expect(entry.contains(document.querySelector('.player small'))).toBe(true)
    fireEvent.click(entry.querySelector(selector)!)
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('lyrics')
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: '返回歌曲列表：World is Mine' }).querySelector(selector)!)
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('songs')
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
    expect(within(tools).getByRole('button', { name: '上传音乐' }).parentElement).toBe(tools)
    const picker=vi.spyOn(screen.getByLabelText('上传音乐文件'),'click')
    fireEvent.click(within(tools).getByRole('button',{name:'上传音乐'}))
    expect(picker).toHaveBeenCalledTimes(1)
    expect(tools.querySelector('.upload-tasks')).toBeTruthy()
    expect(document.querySelector('.track-heading-name .upload')).toBeNull()
    expect(document.querySelector('.playlist-toolbar .upload-tasks')).toBeNull()
    expect(within(screen.getByRole('navigation',{name:'标签组'})).queryByRole('button',{name:'回收站'})).toBeNull()
    expect(screen.queryByText('音乐空间')).toBeNull()
    expect(screen.queryByText('你的私人播放站')).toBeNull()
    fireEvent.click(trash)
    expect(trash.getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('heading', { name: '回收站' })).toBeTruthy()
  })
  it('toggles lyrics and songs from the player without resetting playback or tag selection', async () => {
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('group', { name: '歌曲 World is Mine' }))
    expect(screen.queryByRole('group', { name: '播放视图' })).toBeNull()
    const recovery = usePlayer.getState().recovery
    const tagSelection = usePlayer.getState().tagSelection
    usePlayer.setState({ position: 42 })
    fireEvent.click(screen.getByRole('button', { name: '查看歌词：World is Mine' }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('lyrics')
    expect(usePlayer.getState().playing).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: '返回歌曲列表：World is Mine' }))
    expect(document.querySelector('.music-workspace')?.getAttribute('data-view')).toBe('songs')
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().position).toBe(42)
    expect(usePlayer.getState().recovery).toEqual(recovery)
    expect(usePlayer.getState().tagSelection).toEqual(tagSelection)
  })

  it('plays by clicking the song row and resumes the current song without resetting progress', async () => {
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    const row = await screen.findByRole('group', { name: '歌曲 World is Mine' })
    fireEvent.keyDown(row, { key })
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(usePlayer.getState().playing).toBe(true)
  })

  it('discards selected cover and audio files on cancel without uploading either', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:preview'); URL.revokeObjectURL = vi.fn()
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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
    mockLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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

  it('hides same-title album repetition while keeping distinct album information', async () => {
    const repeated = { ...track, id: 'repeated', title: '嘘の火花', artist: '96猫', album: ' 嘘の火花 ' }
    const distinct = { ...track, id: 'distinct', title: 'あなたに出会わなければ', artist: 'Aimer', album: 'Sleepless Nights' }
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [repeated, distinct] }])
    renderApp()
    const first = await screen.findByText(repeated.title)
    expect(first.closest('.track')?.querySelector('small')?.textContent).toBe('96猫')
    expect(screen.getAllByText(repeated.title)).toHaveLength(1)
    expect(screen.getByText(distinct.title).closest('.track')?.querySelector('small')?.textContent).toBe('Aimer · Sleepless Nights')
  })

  it('previews a manually edited offset and saves the raw lyrics without an offset control', async () => {
    const lyrics = '[offset:0]\n[00:01]First\n[00:04]Second'
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [{ ...track, lyrics }] }])
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
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
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
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    await screen.findByLabelText('选择 World is Mine')
    expect(screen.queryByRole('button', { name: '删除' })).toBeNull()
    expect(screen.queryByRole('button', { name: '设置标签' })).toBeNull()
    expect(screen.queryByLabelText('从歌单移除 World is Mine')).toBeNull()
    fireEvent.click(screen.getByLabelText('选择 World is Mine'))
    expect(screen.getByRole('button', { name: '设置标签' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(api.deleteTrack).toHaveBeenCalledWith('track-1'))
    expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('将进入回收站'))
  })

  it('keeps the selection and sends no requests when deletion is cancelled', async () => {
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderApp()
    fireEvent.click(await screen.findByLabelText('选择 World is Mine'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect(api.deleteTrack).not.toHaveBeenCalled()
    expect((screen.getByLabelText('选择 World is Mine') as HTMLInputElement).checked).toBe(true)
  })

  it('keeps failed deletion selections for retry without repeating successful requests', async () => {
    const second = { ...track, id: 'second', title: 'Second' }
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track, second] }]).mockResolvedValueOnce(asLibrary([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [second] }]))
    vi.mocked(api.deleteTrack).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('failed'))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderApp()
    fireEvent.click(await screen.findByLabelText('全选歌曲'))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect((await screen.findByRole('alert')).textContent).toContain('已删除 1 首，1 首失败')
    expect((screen.getByLabelText('选择 Second') as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await waitFor(() => expect(vi.mocked(api.deleteTrack).mock.calls).toEqual([[track.id], ['second'], ['second']]))
  })

  it('edits title and artist together with song settings', async () => {
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [track] }])
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: '设置 World is Mine' }))
    expect(screen.queryByRole('button', { name: '移入回收站' })).toBeNull()
    fireEvent.change(screen.getByLabelText('歌曲名称'), { target: { value: '新名称' } })
    fireEvent.change(screen.getByLabelText('歌手'), { target: { value: '新歌手' } })
    fireEvent.click(screen.getByText('保存'))
    await waitFor(() => expect(api.updateTrack).toHaveBeenCalledWith(expect.objectContaining({ title: '新名称', artist: '新歌手' })))
  })

  it('sorts by song title rather than filename without manual move controls', async () => {
    mockLibraryOnce([{ id: 'default', name: '默认歌单', isDefault: true, tracks: [
      { ...track, id: 'ten', title: '歌曲10', fileName: '1.mp3', uploadedAt: '2026-11-09T00:00:00Z' },
      { ...track, id: 'two', title: '歌曲2', fileName: '99.mp3', uploadedAt: '2026-11-08T00:00:00Z' },
    ] }])
    renderApp()
    await screen.findByText('歌曲10')
    const titles = () => Array.from(document.querySelectorAll('.track strong')).map((element) => element.textContent)
    expect(titles()).toEqual(['歌曲10', '歌曲2'])
    expect(screen.queryByRole('group', { name: '排序选项' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '排序' }))
    expect(screen.queryByRole('button', { name: /文件名/ })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '歌曲名 · 正序' }))
    expect(screen.queryByRole('group', { name: '排序选项' })).toBeNull()
    expect(titles()).toEqual(['歌曲2', '歌曲10'])
    fireEvent.click(screen.getByRole('button', { name: '排序' }))
    fireEvent.click(screen.getByRole('button', { name: '歌曲名 · 逆序' }))
    expect(titles()).toEqual(['歌曲10', '歌曲2'])
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
