export type LyricsLayout = 'timestamp' | 'separate'
export type LyricsPrimaryLine = 'first' | 'last'

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
  lyricsLayout?: LyricsLayout
  lyricsPrimaryLine?: LyricsPrimaryLine
  coverUrl?: string
  audioUrl: string
  originalAudioUrl?: string
  originalContentType?: string
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
  playlistId?: string
  requiresConfirmation?: boolean
  conflictTitle?: string
  id: string
  fileName: string
  requestKey?: string
  kind?: 'upload' | 'repair' | 'cover' | 'preflight'
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
  expandedSearch?: boolean
  queryTitle: string
  queryArtist: string
  candidates: LyricsCandidate[]
}

export type PlaybackMode = 'random' | 'repeat-one'

export interface CurrentUser { id: string; isDefault: boolean }

export interface CoverCandidate {
  id: string
  title: string
  artist: string
  date?: string
  country?: string
  version?: string
  durationSeconds?: number
  score: number
}
export interface CoverSearchInput { title: string; artist: string; album: string }
export interface CoverSearchResult {
  queryTitle: string
  queryArtist: string
  candidates: CoverCandidate[]
  expanded: boolean
  partial: boolean
}

export interface PlaybackState {
  playlistID: string | null
  trackID: string | null
  audioVersion: number | null
  mode: PlaybackMode
  playing: boolean
  masterVolume: number
  position: number
  recovery: Record<string, number>
}

export interface PlaybackSnapshot {
  version: 2
  userId: string
  sessionId: string
  savedAt: number
  state: PlaybackState
}
