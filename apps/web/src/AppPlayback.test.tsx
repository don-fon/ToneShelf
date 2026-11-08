import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { App } from './App'
import { usePlayer } from './player-store'

const audio = vi.hoisted(() => ({
  element: null as HTMLAudioElement | null,
  load: vi.fn(),
  seek: vi.fn(),
  volume: vi.fn(),
  play: vi.fn().mockResolvedValue(undefined),
  pause: vi.fn(),
}))
vi.mock('./platform/audio', () => ({ WebAudioEngine: class {
  constructor() { audio.element = document.createElement('audio') }
  load(url: string) { audio.load(url) }
  seek(seconds: number) { audio.seek(seconds); audio.element!.currentTime = seconds }
  setVolume(value: number) { audio.volume(value) }
  play() { return audio.play() }
  pause() { audio.pause() }
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
