import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Track, UploadJob } from '@music/domain'
import { api } from './api'

type PendingFile = { file: File; job: UploadJob }

export function useSongSettingsSave(onSaved: () => void) {
  const query = useQueryClient()
  const [coverFile, setCoverFile] = useState<File | null>(null)
  const [audioFile, setAudioFile] = useState<File | null>(null)
  const [coverInvalid, setCoverInvalid] = useState(false)
  const [status, setStatus] = useState('')
  const pending = useRef<{ audio?: PendingFile; cover?: PendingFile }>({})
  const savedSettings = useRef<Track | null>(null)

  const refresh = () => Promise.all(['library', 'playlists', 'quota', 'jobs'].map((key) => query.invalidateQueries({ queryKey: [key] })))
  const applyFile = async (kind: 'audio' | 'cover', trackID: string, file: File) => {
    const label = kind === 'audio' ? '音频' : '封面'
    setStatus(`正在保存${label}…`)
    let task = pending.current[kind]
    if (!task || task.file !== file) {
      const job = await (kind === 'audio' ? api.replaceAudio(trackID, file) : api.replaceCover(trackID, file))
      task = { file, job }; pending.current[kind] = task
      void query.invalidateQueries({ queryKey: ['jobs'] })
    }
    let job = task.job
    while (!['succeeded', 'failed'].includes(job.state)) {
      const jobs = await api.jobs()
      const updated = jobs.find((item) => item.id === job.id)
      if (!updated) throw new Error(`${label}任务状态暂不可用，请重试查询`)
      job = updated; task.job = job
      if (!['succeeded', 'failed'].includes(job.state)) await new Promise((resolve) => setTimeout(resolve, 1500))
    }
    delete pending.current[kind]
    if (job.state === 'failed') throw new Error(job.error || `${label}保存失败`)
    if (kind === 'audio') setAudioFile(null)
    else setCoverFile(null)
  }

  const save = useMutation({
    mutationFn: async (track: Track) => {
      setStatus('正在保存歌曲设置…')
      if (savedSettings.current !== track) {
        await api.updateTrack(track)
        savedSettings.current = track
      }
      // Apply an explicitly chosen cover after the audio task has completed.
      try {
        if (audioFile) await applyFile('audio', track.id, audioFile)
        if (coverFile) await applyFile('cover', track.id, coverFile)
      } catch (reason) {
        throw new Error(`歌曲设置已保存，文件替换未全部完成：${reason instanceof Error ? reason.message : String(reason)}。可重试保存；取消不会撤销已完成的保存。`)
      }
    },
    onSuccess: onSaved,
    onSettled: async () => { setStatus(''); await refresh() },
  })
  return {
    save, status, coverFile, setCoverFile, audioFile, setAudioFile, coverInvalid, setCoverInvalid,
    reset: () => {
      save.reset(); setCoverFile(null); setAudioFile(null); setCoverInvalid(false); setStatus(''); pending.current = {}; savedSettings.current = null
    },
  }
}
