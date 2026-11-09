import { afterEach, describe, expect, it, vi } from 'vitest'
import { WebAudioEngine } from './audio'

afterEach(() => vi.unstubAllGlobals())

function setup(supported = true) {
  const audio = document.createElement('audio')
  let ready = 0
  Object.defineProperty(audio, 'readyState', { get: () => ready })
  vi.spyOn(audio, 'canPlayType').mockReturnValue(supported ? 'probably' : '')
  const play = vi.spyOn(audio, 'play').mockResolvedValue(undefined)
  const pause = vi.spyOn(audio, 'pause').mockImplementation(() => {})
  vi.stubGlobal('Audio', vi.fn(function () { return audio }))
  return {
    engine: new WebAudioEngine(), audio, play, pause,
    metadata: () => { ready = 1; audio.dispatchEvent(new Event('loadedmetadata')) },
  }
}

describe('original audio with fallback', () => {
  it('prefers a supported original and applies restored progress after metadata', () => {
    const { engine, audio, metadata } = setup()
    engine.load('/fallback.mp3', { url: '/original.flac', contentType: 'audio/flac' })
    engine.seek(48)
    expect(audio.src).toMatch(/original\.flac$/)
    expect(audio.currentTime).toBe(0)
    engine.pause()
    expect(engine.position()).toBe(48) // Persistence must retain pending seeks before metadata arrives.
    metadata()
    expect(audio.currentTime).toBe(48)
    expect(engine.position()).toBe(48)
  })

  it('selects the fallback immediately for an unsupported format', () => {
    const { engine, audio } = setup(false)
    engine.load('/fallback.mp3', { url: '/original.wma', contentType: 'audio/x-ms-wma' })
    expect(audio.src).toMatch(/fallback\.mp3$/)
  })

  it('falls back once after a decode error while preserving position, volume, and play intent', async () => {
    const { engine, audio, metadata, play } = setup()
    engine.load('/fallback.mp3', { url: '/original.flac', contentType: 'audio/flac' })
    engine.setVolume(.4)
    metadata()
    audio.currentTime = 60
    audio.dispatchEvent(new Event('timeupdate'))
    await engine.play()
    audio.dispatchEvent(new Event('error'))
    expect(audio.src).toMatch(/fallback\.mp3$/)
    metadata()
    expect(audio.currentTime).toBe(60)
    expect(audio.volume).toBe(.4)
    expect(play).toHaveBeenCalledTimes(2)
    audio.dispatchEvent(new Event('error'))
    expect(play).toHaveBeenCalledTimes(2)
    engine.load('/fallback.mp3', { url: '/original.flac', contentType: 'audio/flac' })
    expect(audio.src).toMatch(/fallback\.mp3$/)
  })

  it('retries a format rejection using the fallback, but does not bypass autoplay restrictions', async () => {
    const { engine, audio, play } = setup()
    engine.load('/fallback.mp3', { url: '/original.flac', contentType: 'audio/flac' })
    play.mockRejectedValueOnce(new DOMException('format', 'NotSupportedError'))
    await engine.play()
    expect(audio.src).toMatch(/fallback\.mp3$/)
    engine.load('/second.mp3', { url: '/second.flac', contentType: 'audio/flac' })
    play.mockRejectedValueOnce(new DOMException('autoplay', 'NotAllowedError'))
    await expect(engine.play()).rejects.toMatchObject({ name: 'NotAllowedError' })
    expect(audio.src).toMatch(/second\.flac$/)
  })
})
