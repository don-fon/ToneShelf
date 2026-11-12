import { useEffect, useRef } from 'react'
import type { Track } from '@music/domain'

interface Options {
  track: Track | null
  playing: boolean
  audio: HTMLAudioElement
  getPosition: () => number
  play: () => void
  pause: () => void
  next: () => void
  seek: (seconds: number) => void
}

// Browsers may expose only part of Media Session or reject individual actions.
const supported = (operation: () => void) => { try { operation() } catch { /* Keep ordinary web playback available. */ } }

export function useMediaSession(options: Options) {
  const latest = useRef(options)
  useEffect(() => { latest.current = options }, [options])
  const session = navigator.mediaSession
  const { track, playing, audio } = options
  const hasTrack = track !== null

  useEffect(() => {
    if (!session) return
    const seek = (seconds: number) => {
      const current = latest.current
      if (!current.track || !Number.isFinite(seconds)) return
      const start = current.track.playbackStartSeconds
      const end = current.track.playbackEndSeconds ?? current.track.durationSeconds
      current.seek(Math.max(start, Math.min(end, seconds)))
      updatePosition()
    }
    const handlers: Partial<Record<MediaSessionAction, MediaSessionActionHandler>> = {
      play: () => { if (latest.current.track) latest.current.play() },
      pause: () => { if (latest.current.track) latest.current.pause() },
      stop: () => { if (latest.current.track) latest.current.pause() },
      nexttrack: () => { if (latest.current.track) latest.current.next() },
      seekto: (details) => { if (details.seekTime !== undefined) seek(details.seekTime) },
      seekbackward: (details) => seek(latest.current.getPosition() - (details.seekOffset ?? 5)),
      seekforward: (details) => seek(latest.current.getPosition() + (details.seekOffset ?? 5)),
    }
    const actions = [...Object.keys(handlers), 'previoustrack'] as MediaSessionAction[]
    for (const action of actions) supported(() => session.setActionHandler(action, hasTrack ? handlers[action] ?? null : null))

    function updatePosition() {
      if (!session) return
      const current = latest.current
      const duration = Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : current.track?.durationSeconds
      const position = current.getPosition()
      const playbackRate = audio.playbackRate
      supported(() => {
        if (!current.track || !duration || !Number.isFinite(duration) || !Number.isFinite(position) || !Number.isFinite(playbackRate) || playbackRate <= 0) session.setPositionState?.()
        else session.setPositionState?.({ duration, playbackRate, position: Math.max(0, Math.min(duration, position)) })
      })
    }
    const onPlayback = () => {
      supported(() => { session.playbackState = latest.current.track ? (audio.paused ? 'paused' : 'playing') : 'none' })
      updatePosition()
    }
    const positionEvents = ['timeupdate', 'loadedmetadata', 'durationchange', 'seeked', 'ratechange']
    for (const name of positionEvents) audio.addEventListener(name, updatePosition)
    audio.addEventListener('play', onPlayback)
    audio.addEventListener('pause', onPlayback)
    updatePosition()
    return () => {
      for (const action of actions) supported(() => session.setActionHandler(action, null))
      for (const name of positionEvents) audio.removeEventListener(name, updatePosition)
      audio.removeEventListener('play', onPlayback)
      audio.removeEventListener('pause', onPlayback)
      supported(() => { session.metadata = null; session.playbackState = 'none'; session.setPositionState?.() })
    }
  }, [session, audio, hasTrack])

  useEffect(() => {
    if (!session) return
    supported(() => {
      session.metadata = track && typeof MediaMetadata !== 'undefined' ? new MediaMetadata({
        title: track.title,
        artist: track.artist,
        album: track.album,
        artwork: track.coverUrl ? [{ src: new URL(track.coverUrl, window.location.href).href }] : [],
      }) : null
    })
  }, [session, track?.id, track?.title, track?.artist, track?.album, track?.coverUrl, hasTrack])

  useEffect(() => {
    if (session) supported(() => { session.playbackState = hasTrack ? (playing ? 'playing' : 'paused') : 'none' })
  }, [session, hasTrack, playing])
}
