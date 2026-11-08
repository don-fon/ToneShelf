import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export const audioAccept = 'audio/*,.aac,.m4a,.m4b,.mp4,.flac,.ogg,.opus,.wma,.wav'
type LocalUpload = { id: string; name: string; progress: number; error?: string; jobID?: string }
let nextUploadID = 0
const labels = { receiving: '上传中', queued: '等待处理', processing: '处理中', succeeded: '成功', failed: '失败' }

export function UploadTasks() {
  const query = useQueryClient()
  const [local, setLocal] = useState<LocalUpload[]>([])
  const { data: jobs = [], error } = useQuery({
    queryKey: ['jobs'],
    queryFn: api.jobs,
    refetchInterval: (queryState) => {
      const receiving = local.some((item) => !item.error && !item.jobID)
      const processing = queryState.state.data?.some((job) => ['receiving', 'queued', 'processing'].includes(job.state))
      return receiving || processing ? 1500 : false
    },
  })
  const snapshot = jobs.map((job) => job.id + job.state).join('|')
  const reservations = jobs.reduce((total, job) => total + job.reservedBytes, 0)
  const previousSnapshot = useRef<string | undefined>(undefined)
  useEffect(() => {
    const previous = previousSnapshot.current
    previousSnapshot.current = snapshot
    if (previous === undefined) return
    void query.invalidateQueries({ queryKey: ['playlists'] })
    void query.invalidateQueries({ queryKey: ['quota'] })
  }, [snapshot, query])
  useEffect(() => { void query.invalidateQueries({ queryKey: ['quota'] }) }, [reservations, query])
  const select = (files: File[]) => {
    for (const file of files) {
      const id = `upload-${Date.now()}-${nextUploadID++}`
      setLocal((items) => [...items, { id, name: file.name, progress: 0 }])
      const update = (patch: Partial<LocalUpload>) => setLocal((items) => items.map((item) => item.id === id ? { ...item, ...patch } : item))
      void api.upload(file, (progress) => update({ progress }), id).then((job) => {
        update({ jobID: job.id })
        void query.invalidateQueries({ queryKey: ['jobs'] })
      }).catch((reason: unknown) => {
        update({ error: reason instanceof Error ? reason.message : '上传失败' })
        void query.invalidateQueries({ queryKey: ['jobs'] })
        void query.invalidateQueries({ queryKey: ['quota'] })
      })
    }
  }
  const userJobs = jobs.filter((job) => job.kind !== 'repair')
  const pending = local.filter((item) => !userJobs.some((job) => job.id === item.jobID || (job.requestKey === item.id && job.state !== 'receiving')))
  const visibleJobs = userJobs.filter((job) => job.state !== 'receiving' || !pending.some((item) => item.id === job.requestKey))
  return <>
    <input className="upload-input" id="music-upload" aria-label="上传音乐文件" type="file" multiple accept={audioAccept} onChange={(event) => { select(Array.from(event.target.files ?? [])); event.target.value = '' }}/>
    {(pending.length > 0 || visibleJobs.length > 0 || error) && <details className="upload-tasks" open={pending.length > 0 || visibleJobs.some((job) => ['receiving', 'queued', 'processing', 'failed'].includes(job.state))}>
      <summary>上传任务 <span>{pending.length + visibleJobs.length}</span></summary>
      {error && <p className="error">无法获取任务状态：{error.message}</p>}
      <div className="upload-task-list">
        {pending.map((item) => <div className="upload-task" key={item.id}><span>{item.name}</span><small className={item.error ? 'error' : ''}>{item.error || `上传中 ${item.progress}%`}</small></div>)}
        {visibleJobs.map((job) => <div className="upload-task" key={job.id}><span>{job.fileName}{job.targetTrackId && <small>（替换音频）</small>}</span><small className={job.state === 'failed' ? 'error' : ''}>{labels[job.state]}{job.error ? `：${job.error}` : ''}</small></div>)}
      </div>
    </details>}
  </>
}
