import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { createUploadKey } from './platform/upload-session'
import type { UploadJob } from '@music/domain'
import { createPortal } from 'react-dom'

export const audioAccept = 'audio/*,.aac,.m4a,.m4b,.mp4,.flac,.ogg,.opus,.wma,.wav'
type LocalUpload = { id: string; name: string; progress: number; error?: string; jobID?: string }
let nextCheckID = 0
const labels = { receiving: '上传中', queued: '等待处理', processing: '处理中', succeeded: '成功', failed: '失败' }

export function UploadTasks({ playlistID, anchor, lyricsView = false }: { playlistID?: string; anchor?: HTMLElement | null; lyricsView?: boolean } = {}) {
  const query = useQueryClient()
  const [compact, setCompact] = useState(false)
  useEffect(() => {
    const media = window.matchMedia?.('(max-width:1100px)')
    if (!media) return
    setCompact(media.matches)
    const update = () => setCompact(media.matches)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  const tracked = useRef(new Set<string>())
  const notified = useRef(new Set<string>())
  const [recent, setRecent] = useState<string[]>([])
  const [dismissed, setDismissed] = useState<string[]>([])
  const [ignoredError, setIgnoredError] = useState<Error | null>(null)
  const [expanded, setExpanded] = useState(false)
  const activity = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const close = () => {
      const details = activity.current?.querySelector('details')
      if (details) details.open = false
      setExpanded(false)
    }
    const outside = (event: PointerEvent) => {
      if (activity.current?.querySelector('details')?.open && event.target instanceof Node && !activity.current.contains(event.target)) close()
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || !activity.current?.querySelector('details')?.open) return
      close()
      activity.current?.querySelector('summary')?.focus()
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', escape)
    }
  }, [])
  const [local, setLocal] = useState<LocalUpload[]>([])
  const [deciding, setDeciding] = useState<string[]>([])
  const [decisionError, setDecisionError] = useState('')
  const [checking, setChecking] = useState<{ id: number; name: string; progress: number }[]>([])
  const [conflicts, setConflicts] = useState<UploadJob[]>([])
  const [confirming, setConfirming] = useState(false)
  const [checkError, setCheckError] = useState('')
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
    void query.invalidateQueries({ queryKey: ['library'] })
    void query.invalidateQueries({ queryKey: ['quota'] })
  }, [snapshot, query])
  useEffect(() => { void query.invalidateQueries({ queryKey: ['quota'] }) }, [reservations, query])
  const enqueue = async (prepared: UploadJob) => {
    const id = createUploadKey()
    tracked.current.add(prepared.id)
    setLocal((items) => [...items, { id, name: prepared.fileName, progress: 100, jobID: prepared.id }])
    try {
      await api.resolveUpload(prepared.id, true, id)
      await query.cancelQueries({ queryKey: ['jobs'] })
      await query.invalidateQueries({ queryKey: ['jobs'] })
    } catch (reason) {
      setLocal((items) => items.map((item) => item.id === id ? { ...item, error: reason instanceof Error ? reason.message : '上传失败' } : item))
      await api.resolveUpload(prepared.id, false).catch(() => {})
      void query.invalidateQueries({ queryKey: ['quota'] })
    }
  }
  const choose = async (overwrite: boolean) => {
    const prepared = conflicts[0]
    if (!prepared) return
    setConfirming(true); setCheckError('')
    try {
      if (overwrite) await enqueue(prepared)
      else await api.resolveUpload(prepared.id, false)
      setConflicts((items) => items.filter((item) => item.id !== prepared.id))
      void query.invalidateQueries({ queryKey: ['quota'] })
    } catch (reason) { setCheckError(reason instanceof Error ? reason.message : '操作失败，请重试') }
    finally { setConfirming(false) }
  }
  const select = (files: File[]) => {
    for (const file of files) {
      const id = nextCheckID++
      setChecking((items) => [...items, { id, name: file.name, progress: 0 }])
      void api.prepareUpload(file, (progress) => setChecking((items) => items.map((item) => item.id === id ? { ...item, progress } : item)), playlistID).then(async (job) => {
        if (job.requiresConfirmation) setConflicts((items) => [...items, job])
        else await enqueue(job)
      }).catch((reason: unknown) => {
        setCheckError(reason instanceof Error ? reason.message : '无法检查文件')
      }).finally(() => { setChecking((items) => items.filter((item) => item.id !== id)); void query.invalidateQueries({ queryKey: ['quota'] }) })
    }
  }
  const userJobs = jobs.filter((job) => job.kind !== 'repair' && job.kind !== 'cover' && job.kind !== 'preflight' && !(job.state === 'failed' && job.error === '已取消'))
  useEffect(() => {
    const completed: string[] = []
    for (const job of userJobs) {
      if (job.state !== 'succeeded' && job.state !== 'failed') tracked.current.add(job.id)
      if (job.state === 'succeeded' && tracked.current.has(job.id) && !notified.current.has(job.id)) {
        notified.current.add(job.id); completed.push(job.id)
      }
    }
    if (completed.length) setRecent((ids) => [...ids, ...completed])
    setLocal((items) => items.filter((item) => item.error || !jobs.some((job) => job.id === item.jobID && ['succeeded', 'failed'].includes(job.state))))
  }, [snapshot])
  const resolve = async (id: string, overwrite: boolean) => {
    setDeciding((items) => [...items, id])
    setDecisionError('')
    try {
      await api.resolveUpload(id, overwrite)
      if (!overwrite) setLocal((items) => items.filter((item) => item.jobID !== id))
      await Promise.all(['jobs', 'playlists', 'library', 'quota'].map((key) => query.invalidateQueries({ queryKey: [key] })))
    } catch (reason) {
      setDecisionError(reason instanceof Error ? reason.message : '操作失败，请重试')
    } finally {
      setDeciding((items) => items.filter((item) => item !== id))
    }
  }
  const pending = local.filter((item) => !dismissed.includes(item.id) && (item.error || !jobs.some((job) => job.id === item.jobID || (job.requestKey === item.id && job.state !== 'receiving'))))
  const visibleJobs = userJobs.filter((job) => (job.state !== 'succeeded' || recent.includes(job.id)) && (job.state !== 'failed' || !dismissed.includes(job.id)) && (job.state !== 'receiving' || !pending.some((item) => item.id === job.requestKey)))
  const running = pending.filter((item) => !item.error).length + visibleJobs.filter((job) => !['succeeded', 'failed'].includes(job.state)).length
  const failed = pending.filter((item) => item.error).length + visibleJobs.filter((job) => job.state === 'failed').length
  const queryError = error && error !== ignoredError ? error : null
  const needsChoice = visibleJobs.some((job) => job.requiresConfirmation)
  useEffect(() => { if (failed > 0 || queryError || needsChoice) setExpanded(true) }, [failed, queryError, needsChoice])
  const destination = anchor && !(compact && lyricsView) ? anchor : document.body
  const dismissFailures = () => {
    setDismissed((ids) => [...ids, ...pending.filter((item) => item.error).map((item) => item.id), ...visibleJobs.filter((job) => job.state === 'failed').map((job) => job.id)])
    setIgnoredError(error); setDecisionError('')
  }
  return <>
    <input className="upload-input" id="music-upload" aria-label="上传音乐文件" type="file" multiple accept={audioAccept} onChange={(event) => { select(Array.from(event.target.files ?? [])); event.target.value = '' }}/>
    {createPortal(<>
    {checking.length > 0 && <div className="upload-check-notice" role="status">{checking.length === 1 ? `正在检查 ${checking[0].name} · ${checking[0].progress}%` : `正在检查 ${checking.length} 个文件…`}</div>}
    {checkError && conflicts.length === 0 && <div className="upload-check-notice error" role="alert">{checkError}<button type="button" onClick={() => setCheckError('')} aria-label="关闭检查提示">×</button></div>}
    {conflicts.length > 0 && <div className="modal"><form className="upload-confirm" role="dialog" aria-modal="true" aria-labelledby="upload-confirm-title" onSubmit={(event) => { event.preventDefault(); void choose(true) }}><h2 id="upload-confirm-title">发现同名歌曲</h2><p>已存在“{conflicts[0].conflictTitle}”，是否覆盖音频？</p><p className="upload-confirm-note">歌词、播放设置和歌单关系将保留。</p>{checkError && <p className="error" role="alert">{checkError}</p>}<div className="upload-confirm-actions"><button type="button" disabled={confirming} onClick={() => void choose(false)}>取消</button><button className="primary" disabled={confirming}>覆盖</button></div></form></div>}
    </>, document.body)}
    {(pending.length > 0 || visibleJobs.length > 0 || queryError) && createPortal(<div ref={activity} className={`upload-activity ${destination === document.body ? 'floating' : ''}`}><details className="upload-tasks" open={expanded} onToggle={(event) => setExpanded(event.currentTarget.open)}>
      <summary aria-label="上传任务" title={running ? '上传中' : failed || queryError ? '上传失败' : '上传完成'}><svg className="upload-task-icon" aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d={!running && !failed && !queryError ? 'M5 12l4 4L19 6' : 'M12 16V3m-4 4 4-4 4 4M4 15v5h16v-5'}/></svg><b className="upload-task-label">{running ? '上传中' : failed || queryError ? '上传失败' : '上传完成'}</b>{running > 0 && <span>{running}</span>}</summary>
      <div className="upload-task-content">
      {queryError && <p className="error">无法获取任务状态：{queryError.message}</p>}
      {(failed > 0 || queryError) && <button type="button" className="dismiss-upload-errors" aria-label="关闭失败提示" onClick={dismissFailures}>关闭失败提示</button>}
      {decisionError && <p className="error" role="alert">{decisionError}</p>}
      <div className="upload-task-list">
        {pending.map((item) => <div className="upload-task" key={item.id}><span>{item.name}</span><small className={item.error ? 'error' : ''}>{item.error || `上传中 ${item.progress}%`}</small></div>)}
        {visibleJobs.map((job) => <div className={`upload-task ${job.requiresConfirmation ? 'upload-conflict' : ''}`} key={job.id}><span>{job.fileName}{job.targetTrackId && !job.requiresConfirmation && <small>（替换音频）</small>}</span>{job.requiresConfirmation ? <div><small>已存在“{job.conflictTitle}”，覆盖音频？</small><div className="upload-conflict-actions"><button type="button" disabled={deciding.includes(job.id)} onClick={() => void resolve(job.id, true)}>覆盖</button><button type="button" disabled={deciding.includes(job.id)} onClick={() => void resolve(job.id, false)}>取消</button></div></div> : <small className={job.state === 'failed' ? 'error' : ''}>{labels[job.state]}{job.error ? `：${job.error}` : ''}</small>}</div>)}
      </div>
      </div>
    </details></div>, destination)}
  </>
}
