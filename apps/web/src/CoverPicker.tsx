import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { CoverCandidate, Playlist, Track } from '@music/domain'
import { api } from './api'
import { TrackCover } from './TrackCover'

export function CoverPicker({ track, onBusyChange }: { track: Track; onBusyChange: (busy: boolean) => void }) {
  const query = useQueryClient()
  const [title, setTitle] = useState(track.title)
  const [artist, setArtist] = useState(track.artist)
  const [album, setAlbum] = useState(track.album)
  const [selected, setSelected] = useState<CoverCandidate | null>(null)
  const [failedImages, setFailedImages] = useState<string[]>([])
  const [jobID, setJobID] = useState<string | null>(null)
  const [message, setMessage] = useState('')
  useEffect(() => { setTitle(track.title); setArtist(track.artist); setAlbum(track.album) }, [track.title, track.artist, track.album])
  const [error, setError] = useState('')
  const search = useMutation({ mutationFn: () => api.searchCovers(track.id, { title, artist, album }), onMutate: () => { apply.reset(); setSelected(null); setFailedImages([]); setMessage(''); setError('') } })
  const apply = useMutation({ mutationFn: () => api.selectCover(track.id, selected!.id), onSuccess: async (job) => { setJobID(job.id); await query.invalidateQueries({ queryKey: ['jobs'] }) }, onMutate: () => { setError(''); setMessage('') } })
  const { data: jobs = [] } = useQuery({ queryKey: ['jobs'], queryFn: api.jobs, enabled: jobID !== null, refetchInterval: jobID ? 1500 : false })
  const job = jobs.find((item) => item.id === jobID)
  const busy = apply.isPending || jobID !== null
  useEffect(() => { onBusyChange(busy); return () => onBusyChange(false) }, [busy, onBusyChange])
  useEffect(() => {
    if (!job || !['succeeded', 'failed'].includes(job.state)) return
    setJobID(null)
    if (job.state === 'failed') setError(job.error || '封面保存失败，请重试')
    else { setMessage('封面已保存'); void query.invalidateQueries({ queryKey: ['playlists'] }) }
    void query.invalidateQueries({ queryKey: ['quota'] })
  }, [job, query])
  const stored = query.getQueryData<Playlist[]>(['playlists'])?.flatMap((list) => list.tracks).find((item) => item.id === track.id)
  const preview = (id: string) => `/api/tracks/${encodeURIComponent(track.id)}/cover/candidates/${encodeURIComponent(id)}`

  return <details className="cover-editor"><summary><TrackCover src={stored?.coverUrl ?? track.coverUrl} className="settings-cover"/><span>封面</span><small>在线查找</small></summary>
    <div className="cover-search-fields" onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); if (!busy && !search.isPending && (title.trim() || album.trim())) search.mutate() } }}><label>搜索歌名<input aria-label="封面搜索歌名" value={title} disabled={busy} onChange={(event) => setTitle(event.target.value)}/></label><label>搜索歌手<input aria-label="封面搜索歌手" value={artist} disabled={busy} onChange={(event) => setArtist(event.target.value)}/></label><label>搜索专辑<input aria-label="封面搜索专辑" value={album} disabled={busy} onChange={(event) => setAlbum(event.target.value)}/></label></div>
    <div className="cover-search-actions"><small>搜索词只用于查找封面。</small><button type="button" disabled={search.isPending || busy || (!title.trim() && !album.trim())} onClick={() => search.mutate()}>{search.isPending ? '正在查找…' : '查找封面'}</button></div>
    {(search.error || apply.error || error) && <p className="error" role="alert">{error || search.error?.message || apply.error?.message}</p>}
    {search.data && <><p className="cover-search-note">{search.data.candidates.length ? '请确认歌手、专辑和版本后选择封面。' : '没有找到可用封面，可修改搜索词重试。'}{search.data.expanded && ' 已补充仅歌名搜索。'}{search.data.partial && ' 部分请求失败，可稍后重试。'}</p><div className="cover-candidates" role="group" aria-label="封面候选">{search.data.candidates.map((candidate) => <button type="button" key={candidate.id} aria-pressed={selected?.id === candidate.id} disabled={busy || failedImages.includes(candidate.id)} onClick={() => { setSelected(candidate); setMessage('') }}>
      {failedImages.includes(candidate.id) ? <span className="cover-preview-unavailable">不可用</span> : <img src={preview(candidate.id)} alt={`${candidate.title}封面`} loading="lazy" onError={() => { setFailedImages((ids) => [...ids, candidate.id]); if (selected?.id === candidate.id) setSelected(null) }}/>}<span><strong>{candidate.title}</strong><small>{candidate.artist || '未知歌手'}</small><small>{[candidate.date, candidate.country, candidate.version, candidate.durationSeconds ? `${Math.floor(candidate.durationSeconds / 60)}:${Math.floor(candidate.durationSeconds % 60).toString().padStart(2, '0')}` : ''].filter(Boolean).join(' · ')}</small></span>
    </button>)}</div></>}
    <div className="cover-save"><span role="status">{busy ? '正在保存封面…' : message}</span>{selected && <button type="button" disabled={busy || search.isPending} onClick={() => apply.mutate()}>保存封面</button>}</div>
  </details>
}
