import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Playlist, Track } from '@music/domain'
import { api } from './api'
import { TrackCover } from './TrackCover'

const maxCoverBytes = 8 * 1024 * 1024
const imageAccept = 'image/jpeg,image/png,image/webp,image/gif,image/bmp,.jpg,.jpeg,.png,.webp,.gif,.bmp'

export function CoverPicker({ track, onBusyChange }: { track: Track; onBusyChange: (busy: boolean) => void }) {
  const query = useQueryClient()
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState('')
  const [previewFailed, setPreviewFailed] = useState(false)
  const [jobID, setJobID] = useState<string | null>(null)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    if (!file) { setPreview(''); return }
    const url = URL.createObjectURL(file)
    setPreview(url)
    return () => URL.revokeObjectURL(url)
  }, [file])
  const apply = useMutation({
    mutationFn: () => api.replaceCover(track.id, file!),
    onMutate: () => { setError(''); setMessage('') },
    onSuccess: async (job) => { setJobID(job.id); await query.invalidateQueries({ queryKey: ['jobs'] }) },
  })
  const { data: jobs = [] } = useQuery({ queryKey: ['jobs'], queryFn: api.jobs, enabled: jobID !== null, refetchInterval: jobID ? 1500 : false })
  const job = jobs.find((item) => item.id === jobID)
  const busy = apply.isPending || jobID !== null
  useEffect(() => { onBusyChange(busy); return () => onBusyChange(false) }, [busy, onBusyChange])
  useEffect(() => {
    if (!job || !['succeeded', 'failed'].includes(job.state)) return
    setJobID(null)
    if (job.state === 'failed') setError(job.error || '封面保存失败，请重试')
    else { setMessage('封面已保存'); setFile(null); void query.invalidateQueries({ queryKey: ['playlists'] }) }
    void query.invalidateQueries({ queryKey: ['quota'] })
  }, [job, query])
  const stored = query.getQueryData<Playlist[]>(['playlists'])?.flatMap((list) => list.tracks).find((item) => item.id === track.id)

  return <div className="cover-editor"><button className="settings-file-button" type="button" disabled={busy} title="JPG / PNG / WebP / GIF / BMP，最大 8 MiB" onClick={() => input.current?.click()}><TrackCover src={stored?.coverUrl ?? track.coverUrl} className="settings-cover"/><span>替换封面</span></button>
      <input ref={input} hidden type="file" aria-label="选择封面图片" accept={imageAccept} disabled={busy} onChange={(event) => {
        const selected = event.target.files?.[0]
        event.target.value = ''
        if (!selected) return
        apply.reset(); setMessage(''); setError(''); setPreviewFailed(false); setFile(null)
        if (!selected.size || selected.size > maxCoverBytes) { setError('请选择不超过 8 MiB 的非空图片'); return }
        setFile(selected)
      }}/>
    {file && <div className="cover-upload-preview">{preview && <img src={preview} alt="新封面预览" onError={() => { setPreviewFailed(true); setError('无法预览这张图片，请选择其他图片') }}/>}<span>{file.name}</span></div>}
    {(apply.error || error) && <p className="error" role="alert">{error || apply.error?.message}</p>}
    {(file || busy || message) && <div className="cover-save"><span role="status">{apply.isPending ? '正在上传封面…' : jobID ? '正在处理封面…' : message}</span>{file && <button type="button" disabled={busy || previewFailed} onClick={() => apply.mutate()}>保存封面</button>}</div>}
  </div>
}
