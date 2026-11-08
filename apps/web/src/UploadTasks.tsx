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
  const [deciding, setDeciding] = useState<string[]>([])
  const [decisionError, setDecisionError] = useState('')
  const { data: jobs = [], error } = useQuery({
    queryKey: ['jobs'],
    queryFn: api.jobs,
    refetchInterval: (queryState) => {
      const receiving = local.some((item) => !item.error && !item.jobID)
      const processing = queryState.state.data?.some((job) => !job.requiresConfirmation && ['receiving', 'queued', 'processing'].includes(job.state))
      return receiving || processing ? 1500 : false
    },
  })
  const snapshot = jobs.map((job) => job.id + job.state + Boolean(job.requiresConfirmation)).join('|')
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
  const resolve = async (id: string, overwrite: boolean) => {
    setDeciding((items) => [...items, id])
    setDecisionError('')
    try {
      await api.resolveUpload(id, overwrite)
      await Promise.all(['jobs', 'playlists', 'quota'].map((key) => query.invalidateQueries({ queryKey: [key] })))
    } catch (reason) {
      setDecisionError(reason instanceof Error ? reason.message : '操作失败，请重试')
    } finally {
      setDeciding((items) => items.filter((item) => item !== id))
    }
  }
  const pending = local.filter((item) => !userJobs.some((job) => job.id === item.jobID || (job.requestKey === item.id && job.state !== 'receiving')))
  const visibleJobs = userJobs.filter((job) => job.state !== 'receiving' || !pending.some((item) => item.id === job.requestKey))
  return <>
    <input className="upload-input" id="music-upload" aria-label="上传音乐文件" type="file" multiple accept={audioAccept} onChange={(event) => { select(Array.from(event.target.files ?? [])); event.target.value = '' }}/>
    {(pending.length > 0 || visibleJobs.length > 0 || error) && <details className="upload-tasks" open={pending.length > 0 || visibleJobs.some((job) => ['receiving', 'queued', 'processing', 'failed'].includes(job.state))}>
      <summary aria-label="上传任务" title="上传任务"><svg className="upload-task-icon" aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M12 16V3m-4 4 4-4 4 4M4 15v5h16v-5"/></svg><b className="upload-task-label">上传任务</b> <span>{pending.length + visibleJobs.length}</span></summary>
      <div className="upload-task-content">
      {error && <p className="error">无法获取任务状态：{error.message}</p>}
      {decisionError && <p className="error" role="alert">{decisionError}</p>}
      <div className="upload-task-list">
        {pending.map((item) => <div className="upload-task" key={item.id}><span>{item.name}</span><small className={item.error ? 'error' : ''}>{item.error || `上传中 ${item.progress}%`}</small></div>)}
        {visibleJobs.map((job) => <div className={`upload-task ${job.requiresConfirmation ? 'upload-conflict' : ''}`} key={job.id}><span>{job.fileName}{job.targetTrackId && !job.requiresConfirmation && <small>（替换音频）</small>}</span>{job.requiresConfirmation ? <div><small>已存在“{job.conflictTitle}”，覆盖音频？</small><div className="upload-conflict-actions"><button type="button" disabled={deciding.includes(job.id)} onClick={() => void resolve(job.id, true)}>覆盖</button><button type="button" disabled={deciding.includes(job.id)} onClick={() => void resolve(job.id, false)}>取消</button></div></div> : <small className={job.state === 'failed' ? 'error' : ''}>{job.error === '已取消' ? '已取消' : labels[job.state]}{job.error && job.error !== '已取消' ? `：${job.error}` : ''}</small>}</div>)}
      </div>
      </div>
    </details>}
  </>
}
