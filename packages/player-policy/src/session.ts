import type { PlaybackState, Playlist } from '@music/domain'

export const emptyPlayback = (): PlaybackState => ({ playlistID: null, trackID: null, audioVersion: null, mode: 'random', playing: false, masterVolume: 1, position: 0, recovery: {} })
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const finite = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)

export function readPlayback(value: unknown): PlaybackState {
  const state = emptyPlayback()
  if (!record(value)) return state
  for (const key of ['playlistID', 'trackID'] as const) if (typeof value[key] === 'string' && value[key]) state[key] = value[key]
  if (Number.isSafeInteger(value.audioVersion) && (value.audioVersion as number) > 0) state.audioVersion = value.audioVersion as number
  if (value.mode === 'repeat-one') state.mode = value.mode
  if (Array.isArray(value.tagSelection)) state.tagSelection = [...new Set(value.tagSelection.filter((id): id is string => typeof id === 'string' && !!id))]
  state.playing = value.playing === true
  if (finite(value.position)) state.position = Math.max(0, value.position)
  if (finite(value.masterVolume)) state.masterVolume = Math.max(0, Math.min(1, value.masterVolume))
  if (record(value.recovery)) state.recovery = Object.fromEntries(Object.entries(value.recovery).filter(([, steps]) => Number.isSafeInteger(steps) && (steps as number) >= 0)) as Record<string, number>
  return state
}

export function reconcilePlayback(value: PlaybackState, playlists: Playlist[]): PlaybackState {
  const state = readPlayback(value)
  const playlist = playlists.find((item) => item.id === state.playlistID)
  if (!playlist) return { ...state, playlistID: (playlists.find((item) => item.isDefault) ?? playlists[0])?.id ?? null, trackID: null, audioVersion: null, position: 0, playing: false, recovery: {} }
  const ids = new Set(playlist.tracks.map((track) => track.id))
  state.recovery = Object.fromEntries(Object.entries(state.recovery).filter(([id]) => ids.has(id)))
  const track = playlist.tracks.find((item) => item.id === state.trackID)
  if (!track) return { ...state, trackID: null, audioVersion: null, position: 0, playing: false }
  const start = track.playbackStartSeconds
  const end = track.playbackEndSeconds ?? track.durationSeconds
  state.position = state.audioVersion !== track.audioVersion || state.position >= end ? start : Math.max(start, state.position)
  state.audioVersion = track.audioVersion
  return state
}
