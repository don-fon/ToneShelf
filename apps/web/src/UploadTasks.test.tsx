import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UploadJob } from '@music/domain'
import { api } from './api'
import { UploadTasks } from './UploadTasks'

vi.mock('./api', () => ({ api: { jobs: vi.fn().mockResolvedValue([]), upload: vi.fn() } }))
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); vi.mocked(api.jobs).mockResolvedValue([]) })
const renderTasks = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { ...render(<QueryClientProvider client={client}><UploadTasks/></QueryClientProvider>), client }
}

describe('upload task UI', () => {
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
