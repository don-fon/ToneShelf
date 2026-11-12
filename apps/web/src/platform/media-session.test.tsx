import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { useMediaSession } from './media-session'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

const track = {
  id: 'first', title: '歌曲', artist: '歌手', album: '专辑', coverUrl: '/api/tracks/first/cover?v=1',
  durationSeconds: 120, playbackStartSeconds: 10, playbackEndSeconds: 100,
} as Track

function setup() {
  const handlers = new Map<MediaSessionAction, MediaSessionActionHandler | null>()
  const session = {
    metadata: null as MediaMetadata | null,
    playbackState: 'none',
    setActionHandler: vi.fn((action: MediaSessionAction, handler: MediaSessionActionHandler | null) => { handlers.set(action, handler) }),
    setPositionState: vi.fn(),
  }
  vi.stubGlobal('navigator', { mediaSession: session })
  vi.stubGlobal('MediaMetadata', class { constructor(data: MediaMetadataInit) { Object.assign(this, data) } })
  const audio = document.createElement('audio')
  audio.currentTime = 40
  const options = {
    track: track as Track | null, playing: true, audio, getPosition: () => audio.currentTime,
    play: vi.fn(), pause: vi.fn(), next: vi.fn(), seek: vi.fn((seconds: number) => { audio.currentTime = seconds }),
  }
  const invoke = (action: MediaSessionAction, details = {}) => act(() => handlers.get(action)?.({ action, ...details }))
  return { session, options, handlers, invoke }
}

it('publishes metadata, uses absolute authenticated cover URLs, and updates it on song and cover changes', () => {
  const { session, options } = setup()
  const { rerender } = renderHook((value) => useMediaSession(value), { initialProps: options })
  expect(session.metadata).toMatchObject({ title: '歌曲', artist: '歌手', album: '专辑', artwork: [{ src: new URL(track.coverUrl!, location.href).href }] })
  expect(session.playbackState).toBe('playing')
  rerender({ ...options, playing: false, track: { ...track, id: 'second', title: '第二首', coverUrl: '/new-cover' } })
  expect(session.metadata).toMatchObject({ title: '第二首', artwork: [{ src: new URL('/new-cover', location.href).href }] })
  expect(session.playbackState).toBe('paused')
  rerender({ ...options, track: { ...track, coverUrl: undefined } })
  expect(session.metadata).toMatchObject({ artwork: [] })
})

it('routes explicit play, pause, stop and next commands to the latest callbacks without inventing a previous track', () => {
  const { session, options, handlers, invoke } = setup()
  const { rerender } = renderHook((value) => useMediaSession(value), { initialProps: options })
  invoke('play'); invoke('pause'); invoke('stop'); invoke('nexttrack')
  expect(options.play).toHaveBeenCalledOnce()
  expect(options.pause).toHaveBeenCalledTimes(2)
  expect(options.next).toHaveBeenCalledOnce()
  expect(handlers.get('previoustrack')).toBeNull()
  const next = vi.fn()
  const registrations = session.setActionHandler.mock.calls.length
  rerender({ ...options, next })
  invoke('nexttrack')
  expect(next).toHaveBeenCalledOnce()
  expect(options.next).toHaveBeenCalledOnce()
  expect(session.setActionHandler).toHaveBeenCalledTimes(registrations)
})

it('seeks from the live audio position, respects offsets and clamps every system seek to the current song interval', () => {
  const { options, invoke } = setup()
  renderHook(() => useMediaSession(options))
  invoke('seekforward')
  expect(options.seek).toHaveBeenLastCalledWith(45)
  invoke('seekbackward', { seekOffset: 20 })
  expect(options.seek).toHaveBeenLastCalledWith(25)
  invoke('seekto', { seekTime: 0 })
  expect(options.seek).toHaveBeenLastCalledWith(10)
  invoke('seekto', { seekTime: 500, fastSeek: true })
  expect(options.seek).toHaveBeenLastCalledWith(100)
  options.seek.mockClear()
  invoke('seekto', { seekTime: NaN }); invoke('seekto')
  expect(options.seek).not.toHaveBeenCalled()
})

it('reports the file timeline, live progress and rate without seeking or reloading audio', () => {
  const { options, session } = setup()
  renderHook(() => useMediaSession(options))
  expect(session.setPositionState).toHaveBeenLastCalledWith({ duration: 120, position: 40, playbackRate: 1 })
  options.audio.currentTime = 72
  options.audio.playbackRate = 1.25
  Object.defineProperty(options.audio, 'duration', { configurable: true, value: 121 })
  act(() => { options.audio.dispatchEvent(new Event('loadedmetadata')); options.audio.dispatchEvent(new Event('timeupdate')) })
  expect(session.setPositionState).toHaveBeenLastCalledWith({ duration: 121, position: 72, playbackRate: 1.25 })
  expect(options.seek).not.toHaveBeenCalled()
  act(() => { options.audio.dispatchEvent(new Event('pause')) })
  expect(session.playbackState).toBe('paused')
})

it('clears invalid position state, disables actions without a song and releases private metadata and listeners on unmount', () => {
  const { session, options, handlers } = setup()
  const { rerender, unmount } = renderHook((value) => useMediaSession(value), { initialProps: options })
  options.audio.playbackRate = 0
  act(() => { options.audio.dispatchEvent(new Event('ratechange')) })
  expect(session.setPositionState).toHaveBeenLastCalledWith()
  rerender({ ...options, track: null })
  expect(session.metadata).toBeNull()
  expect(session.playbackState).toBe('none')
  expect([...handlers.values()].every((handler) => handler === null)).toBe(true)
  rerender(options)
  unmount()
  expect(session.metadata).toBeNull()
  expect(session.playbackState).toBe('none')
  expect([...handlers.values()].every((handler) => handler === null)).toBe(true)
  session.setPositionState.mockClear()
  act(() => { options.audio.dispatchEvent(new Event('timeupdate')) })
  expect(session.setPositionState).not.toHaveBeenCalled()
})

it('keeps working when optional metadata, position or individual action support is missing', () => {
  const { options, session, handlers, invoke } = setup()
  vi.stubGlobal('MediaMetadata', undefined)
  session.setActionHandler.mockImplementation((action, handler) => {
    if (action === 'seekto') throw new DOMException('unsupported', 'NotSupportedError')
    // Other actions remain usable even when one is unsupported.
    handlers.set(action, handler)
  })
  session.setPositionState.mockImplementation(() => { throw new TypeError('unsupported') })
  expect(() => renderHook(() => useMediaSession(options))).not.toThrow()
  expect(handlers.get('nexttrack')).toBeTypeOf('function')
  invoke('nexttrack')
  expect(options.next).toHaveBeenCalledOnce()
  invoke('seekto')
})

it('does nothing on browsers without Media Session', () => {
  const { options } = setup()
  vi.stubGlobal('navigator', {})
  expect(() => renderHook(() => useMediaSession(options))).not.toThrow()
  expect(options.play).not.toHaveBeenCalled()
})
