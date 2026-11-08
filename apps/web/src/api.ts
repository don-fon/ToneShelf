import type { LyricsSearchResult, Playlist, Quota, Track, UploadJob } from '@music/domain'

import { uploadFile } from './platform/upload'

type PlaylistPayload = Omit<Playlist, 'tracks'> & { tracks: Track[] | null }

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(body.error || response.statusText)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
export const api = {
  playlists: async () => {
    const playlists = await request<PlaylistPayload[] | null>('/api/playlists')
    return (playlists ?? []).map((playlist) => ({ ...playlist, tracks: playlist.tracks ?? [] }))
  },
  quota: () => request<Quota>('/api/quota'),
  jobs: () => request<UploadJob[]>('/api/jobs'),
  upload: (file: File, onProgress?: (percent: number) => void, requestKey?: string) => uploadFile('/api/tracks', file, onProgress, requestKey),
  replaceAudio: (trackID: string, file: File) => {
    const data = new FormData(); data.append('file', file)
    return request<UploadJob>(`/api/tracks/${trackID}/audio`, { method: 'PUT', body: data })
  },
  updateTrack: (track: Track) => request<Track>(`/api/tracks/${track.id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
      title: track.title,
      artist: track.artist,
      baseRandomWeight: track.baseRandomWeight,
      playbackStartSeconds: track.playbackStartSeconds,
      playbackEndSeconds: track.playbackEndSeconds,
      volumeRatio: track.volumeRatio,
      lyrics: track.lyrics,
      lyricsLayout: track.lyricsLayout,
      lyricsPrimaryLine: track.lyricsPrimaryLine,
    }),
  }),
  searchLyrics: (trackID: string) => request<LyricsSearchResult>(`/api/tracks/${trackID}/lyrics/search`),
  createPlaylist: (name: string) => request<Playlist>('/api/playlists', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  }),
  renamePlaylist: (playlistID: string, name: string) => request<void>(`/api/playlists/${playlistID}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  }),
  deletePlaylist: (playlistID: string) => request<void>(`/api/playlists/${playlistID}`, { method: 'DELETE' }),
  addTrack: (playlistID: string, trackID: string) => request<void>(`/api/playlists/${playlistID}/tracks/${trackID}`, { method: 'POST' }),
  removeTrack: (playlistID: string, trackID: string, confirmDelete = false) => request<{ trackDeleted: boolean }>(`/api/playlists/${playlistID}/tracks/${trackID}?confirmDelete=${confirmDelete}`, { method: 'DELETE' }),
  deleteTrack: (trackID: string) => request<void>(`/api/tracks/${trackID}`, { method: 'DELETE' }),
  trash: async () => (await request<Track[] | null>('/api/trash')) ?? [],
  restoreTrack: (trackID: string) => request<Track>(`/api/tracks/${trackID}/restore`, { method: 'POST' }),
}
