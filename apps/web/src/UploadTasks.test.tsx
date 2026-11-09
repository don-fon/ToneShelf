import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UploadJob } from '@music/domain'
import { api } from './api'
import { UploadTasks } from './UploadTasks'
import { createUploadKey, uploadSession } from './platform/upload-session'

vi.mock('./api', () => ({ api: { jobs: vi.fn().mockResolvedValue([]), upload: vi.fn(), prepareUpload: vi.fn(), resolveUpload: vi.fn() } }))
afterEach(() => { cleanup(); sessionStorage.clear(); vi.useRealTimers(); vi.resetAllMocks(); vi.mocked(api.jobs).mockResolvedValue([]) })
const renderTasks = (playlistID?: string) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { ...render(<QueryClientProvider client={client}><UploadTasks playlistID={playlistID}/></QueryClientProvider>), client }
}

describe('upload task UI', () => {
  it('hides legacy cancellations while keeping real failure diagnostics', async () => {
    vi.mocked(api.jobs).mockResolvedValue([
      { id: 'cancelled', fileName: 'cancelled.mp3', targetTrackId: 'song', state: 'failed', error: '已取消', reservedBytes: 100, createdAt: '' },
      { id: 'failed', fileName: 'failed.mp3', state: 'failed', error: '转码失败', reservedBytes: 0, createdAt: '' },
    ])
    renderTasks()
    expect(await screen.findByText('失败：转码失败')).toBeTruthy()
    expect(screen.queryByText('cancelled.mp3')).toBeNull()
    expect(screen.queryByText('已取消')).toBeNull()
  })

  it('does not resurrect a local upload row after a fallback conflict is cancelled and deleted', async () => {
    const prepared: UploadJob = { id: 'prepared', fileName: 'new.mp3', kind: 'preflight', state: 'queued', reservedBytes: 1, createdAt: '' }
    vi.mocked(api.prepareUpload).mockResolvedValue(prepared)
    vi.mocked(api.resolveUpload).mockImplementation(async (_id, overwrite, key) => {
      vi.mocked(api.jobs).mockResolvedValue(overwrite ? [{ ...prepared, kind: 'upload', requestKey: key, requiresConfirmation: true, conflictTitle: 'song', targetTrackId: 'old' }] : [])
    })
    const view = renderTasks()
    await waitFor(() => expect(view.client.getQueryState(['jobs'])?.status).toBe('success'))
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['audio'], 'new.mp3')] } })
    fireEvent.click(await screen.findByRole('button', { name: '取消' }))
    await waitFor(() => expect(document.querySelector('.upload-tasks')).toBeNull())
    expect(screen.queryByText('上传中 100%')).toBeNull()
  })

  it('captures the playlist at file selection even if the user switches during precheck', async () => {
    let ready!: (job: UploadJob) => void
    vi.mocked(api.prepareUpload).mockImplementation(() => new Promise((resolve) => { ready = resolve }))
    const view = renderTasks('playlist-a')
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['audio'], 'new.flac')] } })
    expect(vi.mocked(api.prepareUpload).mock.calls[0][2]).toBe('playlist-a')
    view.rerender(<QueryClientProvider client={view.client}><UploadTasks playlistID="playlist-b"/></QueryClientProvider>)
    await act(async () => { ready({ id: 'prepared', playlistId: 'playlist-a', kind: 'preflight', fileName: 'new.flac', state: 'queued', reservedBytes: 1, createdAt: '' }) })
    expect(api.prepareUpload).toHaveBeenCalledTimes(1)
    expect(api.resolveUpload).toHaveBeenCalledWith('prepared', true, expect.any(String))
  })

  it('refreshes the accepted task even when the initial history request is still pending', async () => {
    let initial!: (jobs: UploadJob[]) => void
    const prepared: UploadJob = { id: 'prepared', kind: 'preflight', fileName: 'new.flac', state: 'queued', reservedBytes: 1, createdAt: '' }
    vi.mocked(api.jobs).mockImplementationOnce(() => new Promise((resolve) => { initial = resolve })).mockResolvedValue([{ ...prepared, kind: 'upload', state: 'processing' }])
    vi.mocked(api.prepareUpload).mockResolvedValue(prepared)
    renderTasks()
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['audio'], 'new.flac')] } })
    expect(await screen.findByText('处理中')).toBeTruthy()
    expect(api.jobs).toHaveBeenCalledTimes(2)
    await act(async () => { initial([]) })
    expect(screen.getByText('处理中')).toBeTruthy()
  })

  it.each([false, true])('asks about a duplicate before creating a visible task: overwrite=%s', async (overwrite) => {
    const prepared: UploadJob = { id: 'prepared', kind: 'preflight', fileName: 'different.flac', conflictTitle: 'Same Song', targetTrackId: 'old', requiresConfirmation: true, state: 'queued', reservedBytes: 100, createdAt: '' }
    vi.mocked(api.prepareUpload).mockResolvedValue(prepared)
    vi.mocked(api.resolveUpload).mockImplementation(async (_id, accepted, key) => {
      if (accepted) vi.mocked(api.jobs).mockResolvedValue([{ ...prepared, kind: 'upload', requiresConfirmation: false, requestKey: key }])
    })
    renderTasks()
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['audio'], 'different.flac')] } })
    const dialog = await screen.findByRole('dialog', { name: '发现同名歌曲' })
    expect(dialog.textContent).toContain('Same Song')
    expect(document.querySelector('.upload-tasks')).toBeNull()
    expect(uploadSession.count()).toBe(0)
    expect(api.resolveUpload).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: overwrite ? '覆盖' : '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(vi.mocked(api.resolveUpload).mock.calls[0].slice(0, 2)).toEqual(['prepared', overwrite])
    expect(uploadSession.count()).toBe(overwrite ? 1 : 0)
    if (overwrite) expect(await screen.findByText('different.flac')).toBeTruthy()
    else expect(document.querySelector('.upload-tasks')).toBeNull()
    expect(api.prepareUpload).toHaveBeenCalledTimes(1)
    expect(api.upload).not.toHaveBeenCalled()
  })

  it('does not badge historical tasks, including thirteen successful uploads', async () => {
    vi.mocked(api.jobs).mockResolvedValue(Array.from({ length: 13 }, (_, i) => ({ id: `old-${i}`, fileName: `old-${i}.mp3`, state: 'succeeded' as const, reservedBytes: 0, createdAt: '' })))
    renderTasks()
    expect(await screen.findByText('old-12.mp3')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')).toBeNull()
  })

  it('keeps the session count after remounting and does not double count server jobs', async () => {
    vi.mocked(api.prepareUpload).mockImplementation(async (_file, _progress) => {
      const job: UploadJob = { id: 'new', fileName: 'new.mp3', state: 'succeeded', reservedBytes: 0, createdAt: '' }
      vi.mocked(api.jobs).mockResolvedValue([job])
      return job
    })
    const view = renderTasks()
    await waitFor(() => expect(view.client.getQueryState(['jobs'])?.status).toBe('success'))
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['a'], 'new.mp3')] } })
    expect(await screen.findByText('成功')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
    view.unmount()
    const restored = renderTasks()
    expect(await screen.findByText('new.mp3')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
    restored.unmount()
    sessionStorage.clear()
    renderTasks()
    expect(await screen.findByText('new.mp3')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')).toBeNull()
  })

  it('includes replacement requests created elsewhere in the same page', async () => {
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'existing', fileName: 'replacement.flac', targetTrackId: 'song', state: 'queued', reservedBytes: 0, createdAt: '' }])
    renderTasks()
    await screen.findByText('replacement.flac')
    act(() => { createUploadKey() })
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
  })

  it.each([true, false])('restores a duplicate prompt and submits overwrite=%s', async (overwrite) => {
    const job: UploadJob = { id: 'duplicate', fileName: 'different.flac', state: 'queued', requiresConfirmation: true, conflictTitle: '同名歌', targetTrackId: 'existing', reservedBytes: 100, createdAt: '' }
    vi.mocked(api.jobs).mockResolvedValue([job])
    vi.mocked(api.resolveUpload).mockImplementation(async () => {
      vi.mocked(api.jobs).mockResolvedValue([{ ...job, state: overwrite ? 'succeeded' : 'failed', error: overwrite ? '' : '已取消', requiresConfirmation: false, reservedBytes: 0 }])
    })
    renderTasks()
    expect(await screen.findByText('已存在“同名歌”，覆盖音频？')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: overwrite ? '覆盖' : '取消' }))
    await waitFor(() => expect(api.resolveUpload).toHaveBeenCalledWith('duplicate', overwrite))
    if (overwrite) expect(await screen.findByText('成功')).toBeTruthy()
    else await waitFor(() => expect(screen.queryByText('different.flac')).toBeNull())
    expect(screen.queryByRole('button', { name: '覆盖' })).toBeNull()
    expect(api.upload).not.toHaveBeenCalled()
  })

  it('retains the prompt and allows retry when overwrite fails', async () => {
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'duplicate', fileName: 'song.flac', state: 'queued', requiresConfirmation: true, conflictTitle: 'song', reservedBytes: 100, createdAt: '' }])
    vi.mocked(api.resolveUpload).mockRejectedValue(new Error('原歌曲名称已改变'))
    renderTasks()
    fireEvent.click(await screen.findByRole('button', { name: '覆盖' }))
    expect((await screen.findByRole('alert')).textContent).toBe('原歌曲名称已改变')
    expect((screen.getByRole('button', { name: '取消' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('stops polling while a duplicate awaits a choice', async () => {
    vi.useFakeTimers()
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'duplicate', fileName: 'song.flac', state: 'queued', requiresConfirmation: true, reservedBytes: 100, createdAt: '' }])
    renderTasks()
    await act(async () => { await vi.advanceTimersByTimeAsync(6000) })
    expect(api.jobs).toHaveBeenCalledTimes(1)
  })

  it.each((['repair', 'cover'] as const).flatMap((kind) => (['queued', 'processing', 'succeeded', 'failed'] as const).map((state) => ({ kind, state }))))('hides internal $kind tasks in state $state', async ({ kind, state }) => {
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'internal', kind, fileName: 'internal.mp3', targetTrackId: 'song', state, reservedBytes: 0, createdAt: '' }])
    const { client } = renderTasks()
    await waitFor(() => expect(client.getQueryState(['jobs'])?.status).toBe('success'))
    expect(screen.queryByText('internal.mp3')).toBeNull()
    expect(document.querySelector('.upload-tasks')).toBeNull()
  })

  it('does not count historical uploads or replacements in the current session badge', async () => {
    vi.mocked(api.jobs).mockResolvedValue([
      { id: 'repair', kind: 'repair', fileName: 'internal.mp3', state: 'processing', reservedBytes: 0, createdAt: '' },
      { id: 'replacement', kind: 'upload', targetTrackId: 'song', fileName: 'replacement.flac', state: 'queued', reservedBytes: 0, createdAt: '' },
    ])
    renderTasks()
    expect(await screen.findByText('replacement.flac')).toBeTruthy()
    expect(screen.getByText('（替换音频）')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')).toBeNull()
    expect(screen.queryByText('internal.mp3')).toBeNull()
  })
  it('stops polling after the active task succeeds', async () => {
    vi.useFakeTimers()
    const job: UploadJob = { id: 'active', fileName: 'active.aac', state: 'processing', reservedBytes: 1, createdAt: '' }
    vi.mocked(api.jobs).mockResolvedValueOnce([job]).mockResolvedValue([{ ...job, state: 'succeeded', reservedBytes: 0 }])
    renderTasks()
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(api.jobs).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1600) })
    expect(api.jobs).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(6000) })
    expect(api.jobs).toHaveBeenCalledTimes(2)
  })

  it('does not poll when all loaded tasks have finished', async () => {
    vi.useFakeTimers()
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'done', fileName: 'done.aac', state: 'succeeded', reservedBytes: 0, createdAt: '' }])
    renderTasks()
    await act(async () => { await vi.advanceTimersByTimeAsync(6000) })
    expect(api.jobs).toHaveBeenCalledTimes(1)
  })
  it('keeps precheck progress out of the task list and only enqueues after checking', async () => {
    let ready!: (job: UploadJob) => void
    vi.mocked(api.prepareUpload).mockImplementation(() => new Promise((resolve) => { ready = resolve }))
    const { client } = renderTasks()
    await waitFor(() => expect(client.getQueryState(['jobs'])?.status).toBe('success'))
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['a'], 'same.aac')] } })
    expect(await screen.findByRole('status')).toBeTruthy()
    expect(document.querySelector('.upload-tasks')).toBeNull()
    expect(api.resolveUpload).not.toHaveBeenCalled()
    const job: UploadJob = { id: 'prepared', kind: 'preflight', fileName: 'same.aac', state: 'queued', reservedBytes: 1, createdAt: '' }
    await act(async () => { ready(job) })
    const requestKey = vi.mocked(api.resolveUpload).mock.calls[0][2]
    expect(requestKey).toBeTruthy()
    act(() => { client.setQueryData(['jobs'], [{ ...job, kind: 'upload', requestKey, state: 'processing' }]) })
    expect(await screen.findByText('处理中')).toBeTruthy()
    expect(screen.getAllByText('same.aac')).toHaveLength(1)
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
  })
  it('uploads multiple files independently and displays progress and failure', async () => {
    let succeed!: (job: UploadJob) => void
    let fail!: (reason: Error) => void
    vi.mocked(api.prepareUpload).mockImplementation((file, progress) => {
      progress?.(50)
      return new Promise((resolve, reject) => { if (file.name === 'a.aac') succeed = resolve; else fail = reject })
    })
    renderTasks()
    const input = screen.getByLabelText('上传音乐文件') as HTMLInputElement
    expect(input.multiple).toBe(true)
    fireEvent.change(input, { target: { files: [new File(['a'], 'a.aac'), new File(['b'], 'b.wav')] } })
    await waitFor(() => expect(api.prepareUpload).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('status').textContent).toContain('正在检查 2 个文件')
    expect(document.querySelector('.upload-tasks summary span')).toBeNull()
    const job: UploadJob = { id: 'job-a', fileName: 'a.aac', state: 'processing', reservedBytes: 1, createdAt: '' }
    vi.mocked(api.jobs).mockResolvedValue([job])
    await act(async () => { succeed(job); fail(new Error('空间不足')) })
    expect(await screen.findByText('处理中')).toBeTruthy()
    expect(screen.getByText('空间不足')).toBeTruthy()
    expect(screen.getAllByText('a.aac').length).toBe(1)
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
  })

  it('loads durable task status after remounting without selected files', async () => {
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'persisted', fileName: 'persisted.aac', state: 'succeeded', reservedBytes: 0, createdAt: '' }])
    const view = renderTasks()
    expect(await screen.findByText('成功')).toBeTruthy()
    view.unmount()
    renderTasks()
    expect(await screen.findByText('persisted.aac')).toBeTruthy()
    expect(api.upload).not.toHaveBeenCalled()
  })
})
