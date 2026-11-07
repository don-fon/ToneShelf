import type { Playlist, Quota, Track } from '@music/domain'

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
  upload: (file: File) => {
    const data = new FormData(); data.append('file', file)
    return request<Track>('/api/tracks', { method: 'POST', body: data })
  },
  updateTrack: (track: Track) => request<Track>(`/api/tracks/${track.id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
      baseRandomWeight: track.baseRandomWeight,
      playbackStartSeconds: track.playbackStartSeconds,
      playbackEndSeconds: track.playbackEndSeconds,
      volumeRatio: track.volumeRatio,
      lyrics: track.lyrics,
    }),
  }),
  createPlaylist: (name: string) => request<Playlist>('/api/playlists', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  }),
  addTrack: (playlistID: string, trackID: string) => request<void>(`/api/playlists/${playlistID}/tracks/${trackID}`, { method: 'POST' }),
}
