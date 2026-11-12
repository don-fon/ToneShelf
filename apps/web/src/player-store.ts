import type { CurrentUser, PlaybackState, Playlist } from '@music/domain'
import { emptyPlayback, reconcilePlayback } from '@music/player-policy'
import { create } from 'zustand'
import { createPlaybackPersistence, sessionStore } from './platform/session'

interface PlayerState extends PlaybackState {
  setTagSelection: (ids: string[] | undefined) => void
  selectPlaylist: (id: string) => void
  selectTrack: (id: string, recovery: PlaybackState['recovery'], audioVersion?: number) => void
  setAudioVersion: (audioVersion: number, position?: number) => void
  setMode: (mode: PlaybackState['mode']) => void
  setPlaying: (playing: boolean) => void
  setMasterVolume: (volume: number) => void
  setPosition: (position: number) => void
  reconcile: (playlists: Playlist[]) => void
}

export const usePlayer = create<PlayerState>()((set, get) => ({
  ...emptyPlayback(),
  setTagSelection: (tagSelection) => set({ tagSelection }),
  selectPlaylist: (playlistID) => set({ playlistID, trackID: null, audioVersion: null, playing: false, position: 0, recovery: {} }),
  selectTrack: (trackID, recovery, audioVersion = 1) => set({ trackID, audioVersion, recovery, playing: true, position: 0 }),
  setAudioVersion: (audioVersion, position = 0) => set({ audioVersion, position }),
  setMode: (mode) => set({ mode }), setPlaying: (playing) => set({ playing }),
  setMasterVolume: (masterVolume) => set({ masterVolume }), setPosition: (position) => set({ position }),
  reconcile: (playlists) => {
    const next = reconcilePlayback(get(), playlists)
    if (JSON.stringify(next) !== JSON.stringify(playbackState(get()))) set(next)
  },
}))

function playbackState(state: PlaybackState): PlaybackState {
  const { playlistID, trackID, audioVersion, mode, playing, masterVolume, position, recovery } = state
  return { playlistID, trackID, audioVersion, mode, playing, masterVolume, position, recovery, ...(state.tagSelection === undefined ? {} : { tagSelection: state.tagSelection }) }
}

const persistence = createPlaybackPersistence(sessionStore)
let unsubscribe: (() => void) | undefined
export function initializePlayerSession(user: CurrentUser) {
  unsubscribe?.()
  persistence.flush()
  usePlayer.setState(persistence.open(user))
  const stop = usePlayer.subscribe((state, previous) => {
    const immediate = state.playlistID !== previous.playlistID || state.trackID !== previous.trackID || state.audioVersion !== previous.audioVersion || state.mode !== previous.mode || state.playing !== previous.playing || state.masterVolume !== previous.masterVolume || state.recovery !== previous.recovery
    if (immediate || state.tagSelection !== previous.tagSelection || state.position !== previous.position) persistence.save(playbackState(state), immediate || state.tagSelection !== previous.tagSelection)
  })
  unsubscribe = stop
  return () => { if (unsubscribe === stop) { persistence.flush(); stop(); unsubscribe = undefined } }
}
export const flushPlayerSession = () => persistence.flush()
