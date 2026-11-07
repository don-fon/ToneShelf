import type { PlaybackMode } from '@music/domain'
import type { RecoveryState } from '@music/player-policy'
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { sessionStore } from './platform/session'

interface PlayerState {
  playlistID: string | null
  trackID: string | null
  audioVersion: number | null
  mode: PlaybackMode
  playing: boolean
  masterVolume: number
  position: number
  recovery: RecoveryState
  selectPlaylist: (id: string) => void
  selectTrack: (id: string, recovery: RecoveryState, audioVersion?: number) => void
  setAudioVersion: (audioVersion: number) => void
  setMode: (mode: PlaybackMode) => void
  setPlaying: (playing: boolean) => void
  setMasterVolume: (volume: number) => void
  setPosition: (position: number) => void
}

export const usePlayer = create<PlayerState>()(persist((set) => ({
  playlistID: null, trackID: null, audioVersion: null, mode: 'random', playing: false, masterVolume: 1, position: 0, recovery: {},
  selectPlaylist: (playlistID) => set({ playlistID, trackID: null, audioVersion: null, playing: false, position: 0, recovery: {} }),
  selectTrack: (trackID, recovery, audioVersion = 1) => set({ trackID, audioVersion, recovery, playing: true, position: 0 }),
  setAudioVersion: (audioVersion) => set({ audioVersion, position: 0 }),
  setMode: (mode) => set({ mode }), setPlaying: (playing) => set({ playing }),
  setMasterVolume: (masterVolume) => set({ masterVolume }), setPosition: (position) => set({ position }),
}), { name: 'music-player-session-v1', storage: createJSONStorage(() => sessionStore) }))
