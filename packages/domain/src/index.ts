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
  fileName: string
  uploadedAt: string
  audioVersion: number
  deletedAt?: string
  purgeEligibleAt?: string
  purgeError?: string
}

export interface Playlist {
  id: string
  name: string
  isDefault: boolean
  tracks: Track[]
}

export interface UploadJob {
  id: string
  fileName: string
  requestKey?: string
  targetTrackId?: string
  trackId?: string
  state: 'receiving' | 'queued' | 'processing' | 'succeeded' | 'failed'
  reservedBytes: number
  error?: string
  createdAt: string
}

export interface Quota {
  usedBytes: number
  reservedBytes: number
  limitBytes: number
  remainingBytes: number
}

export interface LyricsCandidate {
  id: number
  trackName: string
  artistName: string
  albumName: string
  duration: number
  synced: boolean
  lyrics: string
  score: number
}

export interface LyricsSearchResult {
  queryTitle: string
  queryArtist: string
  candidates: LyricsCandidate[]
}

export type PlaybackMode = 'random' | 'repeat-one'
