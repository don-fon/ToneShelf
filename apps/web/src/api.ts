import type { CurrentUser, Library, LyricsSearchResult, Playlist, Quota, Track, UploadJob } from '@music/domain'

import { uploadFile } from './platform/upload'
import { createUploadKey } from './platform/upload-session'
import { authGeneration, notifyAuthExpired } from './platform/auth-events'

export class APIError extends Error {
  constructor(message: string, readonly status: number) { super(message) }
}

type PlaylistPayload = Omit<Playlist, 'tracks'> & { tracks: Track[] | null }

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const generation = authGeneration()
  const response = await fetch(url, init)
  if (!response.ok) {
    if (response.status === 401) notifyAuthExpired(generation)
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new APIError(body.error || response.statusText, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
export const api = {
  login: (account: string) => request<CurrentUser>('/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account }) }),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
  currentUser: () => request<CurrentUser>('/api/current-user'),
  library: () => request<Library>('/api/library'),
  saveTag: (name: string, id?: string) => request<void>(id ? `/api/tags/${id}` : '/api/tags', { method: id ? 'PATCH' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) }),
  deleteTag: (id: string) => request<void>(`/api/tags/${id}`, { method: 'DELETE' }),
  saveTagGroup: (name: string, tagIds: string[], id?: string) => request<void>(id ? `/api/tag-groups/${id}` : '/api/tag-groups', { method: id ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, tagIds }) }),
  deleteTagGroup: (id: string) => request<void>(`/api/tag-groups/${id}`, { method: 'DELETE' }),
  setTrackTags: (trackIds: string[], tagIds: string[], mode: 'add' | 'remove') => request<void>('/api/track-tags', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ trackIds, tagIds, mode }) }),
  playlists: async () => {
    const playlists = await request<PlaylistPayload[] | null>('/api/playlists')
    return (playlists ?? []).map((playlist) => ({ ...playlist, tracks: playlist.tracks ?? [] }))
  },
  quota: () => request<Quota>('/api/quota'),
  jobs: () => request<UploadJob[]>('/api/jobs'),
  resolveUpload: (jobID: string, overwrite: boolean, requestKey?: string) => request<void>(`/api/jobs/${jobID}/resolve`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ overwrite, requestKey }),
  }),
  prepareUpload: (file: File, onProgress?: (percent: number) => void, playlistID?: string) => uploadFile(`/api/uploads/check${playlistID ? `?playlistId=${encodeURIComponent(playlistID)}` : ''}`, file, onProgress),
  upload: (file: File, onProgress?: (percent: number) => void, requestKey?: string) => uploadFile('/api/tracks', file, onProgress, requestKey),
  replaceAudio: (trackID: string, file: File) => {
    const data = new FormData(); data.append('file', file)
    return request<UploadJob>(`/api/tracks/${trackID}/audio`, { method: 'PUT', headers: { 'X-Upload-Key': createUploadKey() }, body: data })
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
  replaceCover: (trackID: string, file: File) => {
    const body = new FormData(); body.append('file', file)
    return request<UploadJob>(`/api/tracks/${trackID}/cover`, { method: 'POST', body })
  },
  searchLyrics: (trackID: string) => request<LyricsSearchResult>(`/api/tracks/${trackID}/lyrics/search`),
  createPlaylist: (name: string) => request<Playlist>('/api/playlists', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  }),
  renamePlaylist: (playlistID: string, name: string) => request<void>(`/api/playlists/${playlistID}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  }),
  deletePlaylist: (playlistID: string) => request<void>(`/api/playlists/${playlistID}`, { method: 'DELETE' }),
  addTrack: (playlistID: string, trackID: string) => request<void>(`/api/playlists/${playlistID}/tracks/${trackID}`, { method: 'POST' }),
  moveTrack: (sourceID: string, targetID: string, trackID: string) => request<void>(`/api/playlists/${sourceID}/tracks/${trackID}/move`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ targetPlaylistId: targetID }),
  }),
  removeTrack: (playlistID: string, trackID: string, confirmDelete = false) => request<{ trackDeleted: boolean }>(`/api/playlists/${playlistID}/tracks/${trackID}?confirmDelete=${confirmDelete}`, { method: 'DELETE' }),
  deleteTrack: (trackID: string) => request<void>(`/api/tracks/${trackID}`, { method: 'DELETE' }),
  trash: async () => (await request<Track[] | null>('/api/trash')) ?? [],
  restoreTrack: (trackID: string) => request<Track>(`/api/tracks/${trackID}/restore`, { method: 'POST' }),
}
