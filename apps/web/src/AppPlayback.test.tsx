import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { App } from './App'
import { initializePlayerSession, usePlayer } from './player-store'
import { api } from './api'
import { playbackStorageKey } from './platform/session'

const audio = vi.hoisted(() => ({
  element: null as HTMLAudioElement | null,
  load: vi.fn(),
  seek: vi.fn(),
  volume: vi.fn(),
  play: vi.fn().mockResolvedValue(undefined),
  pause: vi.fn(),
}))
vi.mock('./platform/audio', () => ({ WebAudioEngine: class {
  constructor() { audio.element = document.createElement('audio'); vi.spyOn(audio.element, 'play').mockResolvedValue(undefined) }
  load(url: string, original?: { url: string; contentType: string }) { if (original) audio.load(url, original); else audio.load(url) }
  seek(seconds: number) { audio.seek(seconds); audio.element!.currentTime = seconds }
  setVolume(value: number) { audio.volume(value) }
  play() { return audio.play() }
  pause() { audio.pause() }
  unload() { audio.pause() }
  position() { return audio.element!.currentTime }
  element() { return audio.element! }
} }))

const track: Track = {
  id: 'song', title: 'Playback test', artist: '', album: '', contentType: 'audio/mpeg', sizeBytes: 1,
  durationSeconds: 120, baseRandomWeight: 1, playbackStartSeconds: 5, playbackEndSeconds: null,
  volumeRatio: .8, lyrics: '', audioUrl: '/api/tracks/song/audio?v=1', audioVersion: 1,
  fileName: 'song.mp3', uploadedAt: '2026-11-08T00:00:00Z',
}
vi.mock('./api', () => ({ api: {
  playlists: vi.fn(async () => [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [track] }]),
  quota: vi.fn(async () => ({ usedBytes: 1, reservedBytes: 0, limitBytes: 1024, remainingBytes: 1023 })),
  trash: vi.fn(async () => []),
  jobs: vi.fn(async () => []),
} }))

afterEach(() => {
  cleanup()
  usePlayer.setState(usePlayer.getInitialState(), true)
  sessionStorage.clear()
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

async function setup(playing = true) {
  usePlayer.setState({ playlistID: 'list', trackID: track.id, audioVersion: 1, position: 48, playing, masterVolume: 1 })
  const query = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={query}><App/></QueryClientProvider>)
  await screen.findByRole('button', { name: '设置 Playback test' })
  await waitFor(() => expect(audio.seek).toHaveBeenCalledWith(48))
  act(() => { usePlayer.getState().setPlaying(playing) })
  audio.element!.currentTime = 48.8 // real audio has advanced beyond the last saved position
  vi.clearAllMocks()
  return query
}

describe('independent playback controls', () => {
  it('toggles playback with Space on the focused song row without restarting it or double handling the key', async () => {
    await setup()
    const row = screen.getByRole('group', { name: '歌曲 Playback test' })
    row.focus()
    const recovery = usePlayer.getState().recovery
    fireEvent.keyDown(row, { key: ' ' })
    expect(usePlayer.getState().playing).toBe(false)
    expect(usePlayer.getState().position).toBe(48.8)
    expect(audio.pause).toHaveBeenCalledOnce()
    fireEvent.keyDown(row, { key: ' ', repeat: true })
    expect(usePlayer.getState().playing).toBe(false)
    fireEvent.keyDown(row, { key: ' ' })
    await waitFor(() => expect(audio.play).toHaveBeenCalledOnce())
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().recovery).toEqual(recovery)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
  })

  it('uses Space to pause the current song even when another song row has focus', async () => {
    const query = await setup()
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [track, { ...track, id: 'second', title: 'Second', audioUrl: '/second' }] }]) })
    const row = await screen.findByRole('group', { name: '歌曲 Second' })
    row.focus()
    fireEvent.keyDown(row, { key: ' ' })
    expect(usePlayer.getState().playing).toBe(false)
    expect(usePlayer.getState().trackID).toBe(track.id)
    expect(audio.load).not.toHaveBeenCalled()
  })

  it('toggles playback with Space without seeking or reloading the current audio', async () => {
    await setup()
    fireEvent.keyDown(document.body, { key: ' ' })
    expect(usePlayer.getState().playing).toBe(false)
    expect(usePlayer.getState().position).toBe(48.8)
    fireEvent.keyDown(document.body, { key: ' ' })
    await waitFor(() => expect(audio.play).toHaveBeenCalledOnce())
    expect(usePlayer.getState().playing).toBe(true)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
  })

  it('seeks from the live audio position and clamps shortcuts to the song playback interval', async () => {
    const query = await setup(false)
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, playbackEndSeconds: 52 }] }]) })
    await waitFor(() => expect(document.querySelector('.controls input')?.getAttribute('max')).toBe('52'))
    audio.element!.currentTime = 48.8
    fireEvent.keyDown(document.body, { key: 'ArrowRight' })
    expect(audio.seek).toHaveBeenLastCalledWith(52)
    fireEvent.keyDown(document.body, { key: 'ArrowLeft' })
    expect(audio.seek).toHaveBeenLastCalledWith(47)
    audio.element!.currentTime = 6
    fireEvent.keyDown(document.body, { key: 'ArrowLeft' })
    expect(audio.seek).toHaveBeenLastCalledWith(5)
    expect(usePlayer.getState().playing).toBe(false)
    expect(audio.load).not.toHaveBeenCalled()
  })

  it('changes and restores master volume with arrows and M without altering progress', async () => {
    await setup()
    fireEvent.keyDown(document.body, { key: 'ArrowDown' })
    expect(usePlayer.getState().masterVolume).toBe(.95)
    fireEvent.keyDown(document.body, { key: 'm' })
    expect(usePlayer.getState().masterVolume).toBe(0)
    fireEvent.keyDown(document.body, { key: 'm' })
    expect(usePlayer.getState().masterVolume).toBe(.95)
    fireEvent.keyDown(document.body, { key: 'ArrowUp' })
    fireEvent.keyDown(document.body, { key: 'ArrowUp' })
    expect(usePlayer.getState().masterVolume).toBe(1)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
    expect(audio.element!.currentTime).toBe(48.8)
  })

  it('uses N for the existing next-song policy and ignores keys while help is open', async () => {
    const query = await setup(false)
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [track, { ...track, id: 'second', title: 'Second', audioUrl: '/second', playbackStartSeconds: 7 }] }]) })
    await screen.findByRole('button', { name: '设置 Second' })
    fireEvent.keyDown(document.body, { key: 'n' })
    expect(usePlayer.getState().trackID).toBe('second')
    fireEvent.keyDown(document.body, { key: '?', shiftKey: true })
    expect(screen.getByRole('dialog', { name: '快捷键' })).toBeTruthy()
    audio.seek.mockClear()
    fireEvent.keyDown(document.body, { key: 'ArrowRight' })
    expect(audio.seek).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '关闭快捷键说明' }))
    expect(screen.queryByRole('dialog', { name: '快捷键' })).toBeNull()
  })
  it('updates artwork without seeking, pausing or reloading audio', async () => {
    const query = await setup()
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, coverUrl: '/api/tracks/song/cover?v=1&c=2' }] }]) })
    await waitFor(() => expect(document.querySelector('.now img')?.getAttribute('src')).toBe('/api/tracks/song/cover?v=1&c=2'))
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
    expect(audio.pause).not.toHaveBeenCalled()
    expect(audio.play).not.toHaveBeenCalled()
    expect(audio.element!.currentTime).toBe(48.8)
  })

  it('waits for the library before validating restored playback and pauses if autoplay is blocked', async () => {
    let resolve!: (value: Awaited<ReturnType<typeof api.playlists>>) => void
    vi.mocked(api.playlists).mockReturnValueOnce(new Promise((done) => { resolve = done }))
    usePlayer.setState({ playlistID: 'list', trackID: track.id, audioVersion: 1, position: 48, playing: true })
    const query = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={query}><App/></QueryClientProvider>)
    await screen.findByText('正在载入音乐空间…')
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().position).toBe(48)
    audio.play.mockRejectedValueOnce(new DOMException('blocked', 'NotAllowedError'))
    await act(async () => { resolve([{ id: 'list', name: 'list', isDefault: true, tracks: [track] }]) })
    await waitFor(() => expect(usePlayer.getState().playing).toBe(false))
    expect(audio.seek).toHaveBeenCalledWith(48)
    expect(usePlayer.getState().trackID).toBe(track.id)
  })

  it('saves progress on pause and page exit without writing on each timeupdate', async () => {
    const stop = initializePlayerSession({ id: 'session-user', isDefault: false })
    try {
      await setup()
      vi.useFakeTimers()
      const writes = vi.spyOn(Storage.prototype, 'setItem')
      for (let i = 0; i < 6; i++) {
        audio.element!.currentTime = 50 + i
        act(() => { audio.element!.dispatchEvent(new Event('timeupdate')) })
        act(() => { vi.advanceTimersByTime(250) })
      }
      expect(writes).not.toHaveBeenCalled()
      act(() => { vi.advanceTimersByTime(500) })
      expect(writes).toHaveBeenCalledTimes(1)
      audio.element!.currentTime = 56.25
      fireEvent.click(screen.getByRole('button', { name: '暂停' }))
      let saved = JSON.parse(sessionStorage.getItem(playbackStorageKey('session-user'))!)
      expect(saved.state.position).toBe(56.25)
      expect(saved.state.playing).toBe(false)
      audio.element!.currentTime = 57
      act(() => { window.dispatchEvent(new Event('pagehide')) })
      saved = JSON.parse(sessionStorage.getItem(playbackStorageKey('session-user'))!)
      expect(saved.state.position).toBe(57)
    } finally { stop(); vi.useRealTimers() }
  })

  it('removes stale random entries when a playlist refresh removes songs', async () => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [track, { ...track, id: 'removed', title: 'Removed song' }] }])
    const query = await setup()
    act(() => {
      usePlayer.setState({ recovery: { song: 0, removed: 1, otherList: 2 } })
      query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [track] }])
    })
    await waitFor(() => expect(usePlayer.getState().recovery).toEqual({ song: 0 }))
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [] }]) })
    await waitFor(() => expect(usePlayer.getState().trackID).toBeNull())
    expect(usePlayer.getState().playing).toBe(false)
    expect(usePlayer.getState().position).toBe(0)
    expect(usePlayer.getState().recovery).toEqual({})
  })

  it('starts weighted random playback in the active playlist when no song is selected', async () => {
    const second = { ...track, id: 'second', title: 'Second', baseRandomWeight: 3, playbackStartSeconds: 7, audioVersion: 2, audioUrl: '/second?v=2' }
    vi.mocked(api.playlists).mockResolvedValueOnce([
      { id: 'other', name: 'Other playlist', isDefault: true, tracks: [{ ...track, id: 'outside', baseRandomWeight: 1000000 }] },
      { id: 'list', name: 'Active playlist', isDefault: false, tracks: [track, second] },
    ])
    usePlayer.setState({ playlistID: 'list', trackID: null, mode: 'random', playing: false })
    const query = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={query}><App/></QueryClientProvider>)
    await screen.findByRole('button', { name: '设置 Second' })
    vi.spyOn(Math, 'random').mockReturnValue(.3)
    const toggle = screen.getByRole('button', { name: '播放' }) as HTMLButtonElement
    expect(toggle.disabled).toBe(false)
    fireEvent.click(toggle)
    await waitFor(() => expect(audio.play).toHaveBeenCalledTimes(1))
    expect(usePlayer.getState().trackID).toBe('second')
    expect(usePlayer.getState().playing).toBe(true)
    expect(usePlayer.getState().audioVersion).toBe(2)
    expect(usePlayer.getState().recovery).toEqual({ second: 0 })
    expect(audio.load).toHaveBeenCalledWith('/second?v=2')
    expect(audio.seek).toHaveBeenCalledWith(7)
    fireEvent.click(screen.getByRole('button', { name: '暂停' }))
    expect(usePlayer.getState().playing).toBe(false)
    expect(usePlayer.getState().trackID).toBe('second')
  })

  it.each(['empty', 'repeat-one'] as const)('keeps play disabled without a selected song for %s', async (scenario) => {
    vi.mocked(api.playlists).mockResolvedValueOnce([{ id: 'list', name: 'Test playlist', isDefault: true, tracks: scenario === 'empty' ? [] : [track] }])
    usePlayer.setState({ playlistID: 'list', trackID: null, playing: false, mode: scenario === 'repeat-one' ? 'repeat-one' : 'random' })
    const query = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={query}><App/></QueryClientProvider>)
    await screen.findByRole('button', { name: /Test playlist/ })
    const toggle = screen.getByRole('button', { name: '播放' }) as HTMLButtonElement
    expect(toggle.disabled).toBe(true)
    fireEvent.click(toggle)
    fireEvent.keyDown(document.body, { key: ' ' })
    expect(audio.play).not.toHaveBeenCalled()
    expect(usePlayer.getState().trackID).toBeNull()
  })

  it('prefers the original source when the server advertises one', async () => {
    const query = await setup()
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, originalAudioUrl: '/original', originalContentType: 'audio/flac' }] }]) })
    await waitFor(() => expect(audio.load).toHaveBeenCalledWith(track.audioUrl, { url: '/original', contentType: 'audio/flac' }))
  })

  it('does not cut full-song playback at the stored duration', async () => {
    await setup()
    audio.element!.currentTime = 121
    act(() => { audio.element!.dispatchEvent(new Event('timeupdate')) })
    expect(usePlayer.getState().trackID).toBe('song')
    expect(usePlayer.getState().position).toBe(121)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.pause).not.toHaveBeenCalled()
  })

  it('still loops a user-defined playback interval at its endpoint', async () => {
    const query = await setup()
    act(() => {
      usePlayer.setState({ mode: 'repeat-one' })
      query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, playbackEndSeconds: 60 }] }])
    })
    await waitFor(() => expect(document.querySelector<HTMLInputElement>('.controls input')!.max).toBe('60'))
    vi.clearAllMocks()
    audio.element!.currentTime = 61
    act(() => { audio.element!.dispatchEvent(new Event('timeupdate')) })
    expect(audio.element!.currentTime).toBe(5)
  })
  it.each([true, false])('changes global volume without seeking or restarting (playing=%s)', async (playing) => {
    await setup(playing)
    fireEvent.change(document.querySelector<HTMLInputElement>('.volume input')!, { target: { value: '.5' } })
    expect(audio.volume).toHaveBeenCalledWith(.4)
    expect(audio.element!.currentTime).toBe(48.8)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
    expect(audio.play).not.toHaveBeenCalled()
    expect(audio.pause).not.toHaveBeenCalled()
  })

  it('pauses and resumes at the live audio position', async () => {
    await setup()
    const toggle = document.querySelector<HTMLButtonElement>('.controls .main')!
    fireEvent.click(toggle)
    expect(audio.pause).toHaveBeenCalledTimes(1)
    fireEvent.click(toggle)
    expect(audio.play).toHaveBeenCalledTimes(1)
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.load).not.toHaveBeenCalled()
    expect(audio.element!.currentTime).toBe(48.8)
  })

  it('applies edited song volume without seeking', async () => {
    const query = await setup()
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, volumeRatio: .5 }] }]) })
    await waitFor(() => expect(audio.volume).toHaveBeenCalledWith(.5))
    expect(audio.seek).not.toHaveBeenCalled()
    expect(audio.play).not.toHaveBeenCalled()
    expect(audio.element!.currentTime).toBe(48.8)
  })

  it('still resets progress when the audio version is replaced', async () => {
    const query = await setup()
    act(() => { query.setQueryData(['playlists'], [{ id: 'list', name: 'Test playlist', isDefault: true, tracks: [{ ...track, audioVersion: 2, audioUrl: '/api/tracks/song/audio?v=2' }] }]) })
    await waitFor(() => expect(audio.seek).toHaveBeenCalledWith(5))
    expect(audio.load).toHaveBeenCalledWith('/api/tracks/song/audio?v=2')
    expect(usePlayer.getState().audioVersion).toBe(2)
  })
})
