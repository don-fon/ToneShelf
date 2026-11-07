export interface Track {
  id: string
  title: string
  artist: string
  album: string
  contentType: string
  sizeBytes: number
  durationSeconds: number
  baseRandomWeight: number
  playbackStartSeconds: number
  playbackEndSeconds: number | null
  volumeRatio: number
  lyrics: string
  audioUrl: string
}

export interface Playlist {
  id: string
  name: string
  isDefault: boolean
  tracks: Track[]
}

export interface Quota {
  usedBytes: number
  limitBytes: number
  remainingBytes: number
}

export type PlaybackMode = 'random' | 'repeat-one'
