import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UploadJob } from '@music/domain'
import { api } from './api'
import { UploadTasks } from './UploadTasks'

vi.mock('./api', () => ({ api: { jobs: vi.fn().mockResolvedValue([]), upload: vi.fn(), resolveUpload: vi.fn() } }))
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); vi.mocked(api.jobs).mockResolvedValue([]) })
const renderTasks = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { ...render(<QueryClientProvider client={client}><UploadTasks/></QueryClientProvider>), client }
}

describe('upload task UI', () => {
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
    expect(await screen.findByText(overwrite ? '成功' : '已取消')).toBeTruthy()
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

  it.each(['queued', 'processing', 'succeeded', 'failed'] as const)('hides internal repair tasks in state %s', async (state) => {
    vi.mocked(api.jobs).mockResolvedValue([{ id: 'repair', kind: 'repair', fileName: 'internal.mp3', targetTrackId: 'song', state, reservedBytes: 0, createdAt: '' }])
    const { client } = renderTasks()
    await waitFor(() => expect(client.getQueryState(['jobs'])?.status).toBe('success'))
    expect(screen.queryByText('internal.mp3')).toBeNull()
    expect(document.querySelector('.upload-tasks')).toBeNull()
  })

  it('counts only user uploads and replacements alongside internal repairs', async () => {
    vi.mocked(api.jobs).mockResolvedValue([
      { id: 'repair', kind: 'repair', fileName: 'internal.mp3', state: 'processing', reservedBytes: 0, createdAt: '' },
      { id: 'replacement', kind: 'upload', targetTrackId: 'song', fileName: 'replacement.flac', state: 'queued', reservedBytes: 0, createdAt: '' },
    ])
    renderTasks()
    expect(await screen.findByText('replacement.flac')).toBeTruthy()
    expect(screen.getByText('（替换音频）')).toBeTruthy()
    expect(document.querySelector('.upload-tasks summary span')?.textContent).toBe('1')
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
  it('merges server reception status with the local progress row', async () => {
    vi.mocked(api.upload).mockImplementation(() => new Promise(() => {}))
    const { client } = renderTasks()
    fireEvent.change(screen.getByLabelText('上传音乐文件'), { target: { files: [new File(['a'], 'same.aac')] } })
    await waitFor(() => expect(api.upload).toHaveBeenCalledTimes(1))
    const requestKey = vi.mocked(api.upload).mock.calls[0][2]
    const job: UploadJob = { id: 'receiving', requestKey, fileName: 'same.aac', state: 'receiving', reservedBytes: 1, createdAt: '' }
    act(() => { client.setQueryData(['jobs'], [job]) })
    expect(screen.getAllByText('same.aac').length).toBe(1)
    act(() => { client.setQueryData(['jobs'], [{ ...job, state: 'failed', error: '空间不足' }]) })
    await screen.findByText('失败：空间不足')
    expect(screen.getAllByText('same.aac').length).toBe(1)
  })
  it('uploads multiple files independently and displays progress and failure', async () => {
    let succeed!: (job: UploadJob) => void
    let fail!: (reason: Error) => void
    vi.mocked(api.upload).mockImplementation((file, progress) => {
      progress?.(50)
      return new Promise((resolve, reject) => { if (file.name === 'a.aac') succeed = resolve; else fail = reject })
    })
    renderTasks()
    const input = screen.getByLabelText('上传音乐文件') as HTMLInputElement
    expect(input.multiple).toBe(true)
    fireEvent.change(input, { target: { files: [new File(['a'], 'a.aac'), new File(['b'], 'b.wav')] } })
    await waitFor(() => expect(api.upload).toHaveBeenCalledTimes(2))
    expect(screen.getAllByText('上传中 50%').length).toBe(2)
    const job: UploadJob = { id: 'job-a', fileName: 'a.aac', state: 'processing', reservedBytes: 1, createdAt: '' }
    vi.mocked(api.jobs).mockResolvedValue([job])
    await act(async () => { succeed(job); fail(new Error('空间不足')) })
    expect(await screen.findByText('处理中')).toBeTruthy()
    expect(screen.getByText('空间不足')).toBeTruthy()
    expect(screen.getAllByText('a.aac').length).toBe(1)
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
