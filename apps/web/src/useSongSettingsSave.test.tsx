import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Track, UploadJob } from '@music/domain'
import { api } from './api'
import { useSongSettingsSave } from './useSongSettingsSave'

vi.mock('./api', () => ({ api: { updateTrack: vi.fn(), replaceAudio: vi.fn(), replaceCover: vi.fn(), jobs: vi.fn() } }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const track = { id: 'song', title: 'Song' } as Track
const audio = new File(['audio'], 'new.mp3', { type: 'audio/mpeg' })
const cover = new File(['image'], 'cover.png', { type: 'image/png' })
const job = (id: string, state: UploadJob['state'] = 'queued', error?: string): UploadJob => ({ id, state, error, fileName: '', createdAt: '', reservedBytes: 0 })
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const saved = vi.fn()
  const hook = renderHook(() => useSongSettingsSave(saved), { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
  return { ...hook, saved }
}

it('saves settings, then finishes audio replacement before applying the chosen cover', async () => {
  const order: string[] = []
  vi.mocked(api.updateTrack).mockImplementation(async () => { order.push('settings'); return track })
  vi.mocked(api.replaceAudio).mockImplementation(async () => { order.push('audio'); return job('audio') })
  vi.mocked(api.replaceCover).mockImplementation(async () => { order.push('cover'); return job('cover') })
  vi.mocked(api.jobs).mockImplementation(async () => { order.push('poll'); return [job('audio', 'succeeded'), job('cover', 'succeeded')] })
  const { result, saved } = setup()
  act(() => { result.current.setAudioFile(audio); result.current.setCoverFile(cover) })
  expect(api.replaceAudio).not.toHaveBeenCalled()
  expect(api.replaceCover).not.toHaveBeenCalled()
  await act(async () => { await result.current.save.mutateAsync(track) })
  expect(order).toEqual(['settings', 'audio', 'poll', 'cover', 'poll'])
  expect(saved).toHaveBeenCalledOnce()
  expect(result.current.audioFile).toBeNull()
  expect(result.current.coverFile).toBeNull()
})

it('does not upload files when song settings validation fails', async () => {
  vi.mocked(api.updateTrack).mockRejectedValue(new Error('歌曲名称无效'))
  const { result, saved } = setup()
  act(() => { result.current.setAudioFile(audio); result.current.setCoverFile(cover) })
  await act(async () => { await expect(result.current.save.mutateAsync(track)).rejects.toThrow('歌曲名称无效') })
  expect(api.replaceAudio).not.toHaveBeenCalled()
  expect(api.replaceCover).not.toHaveBeenCalled()
  expect(saved).not.toHaveBeenCalled()
  expect(result.current.coverFile).toBe(cover)
})

it('retains failed cover drafts for retry without repeating a completed audio replacement', async () => {
  vi.mocked(api.replaceAudio).mockResolvedValue(job('audio', 'succeeded'))
  vi.mocked(api.replaceCover).mockResolvedValueOnce(job('cover', 'failed', '空间不足')).mockResolvedValueOnce(job('cover2', 'succeeded'))
  const { result, saved } = setup()
  act(() => { result.current.setAudioFile(audio); result.current.setCoverFile(cover) })
  await act(async () => { await expect(result.current.save.mutateAsync(track)).rejects.toThrow('歌曲设置已保存，文件替换未全部完成：空间不足') })
  expect(saved).not.toHaveBeenCalled()
  expect(result.current.audioFile).toBeNull()
  expect(result.current.coverFile).toBe(cover)
  await act(async () => { await result.current.save.mutateAsync(track) })
  expect(api.replaceAudio).toHaveBeenCalledOnce()
  expect(api.replaceCover).toHaveBeenCalledTimes(2)
  expect(api.updateTrack).toHaveBeenCalledOnce()
  expect(saved).toHaveBeenCalledOnce()
})

it('resumes querying an accepted job after a connection failure instead of uploading again', async () => {
  vi.mocked(api.replaceCover).mockResolvedValue(job('cover'))
  vi.mocked(api.jobs).mockRejectedValueOnce(new Error('网络断开')).mockResolvedValueOnce([job('cover', 'succeeded')])
  const { result, saved } = setup()
  act(() => result.current.setCoverFile(cover))
  await act(async () => { await expect(result.current.save.mutateAsync(track)).rejects.toThrow('网络断开') })
  await act(async () => { await result.current.save.mutateAsync(track) })
  expect(api.replaceCover).toHaveBeenCalledOnce()
  expect(saved).toHaveBeenCalledOnce()
})
