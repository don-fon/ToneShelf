import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { CoverPicker } from './CoverPicker'
import { api } from './api'

vi.mock('./api', () => ({ api: { replaceCover: vi.fn(), jobs: vi.fn().mockResolvedValue([]) } }))
beforeEach(() => { URL.createObjectURL = vi.fn(() => 'blob:preview'); URL.revokeObjectURL = vi.fn() })
afterEach(() => { cleanup(); vi.resetAllMocks() })
const track = { id: 'song', title: 'Song', artist: 'Singer', album: 'Album' } as Track
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const busy = vi.fn()
  const view = render(<QueryClientProvider client={client}><CoverPicker track={track} onBusyChange={busy}/></QueryClientProvider>)
  return { client, busy, view }
}
const choose = (file = new File(['image'], 'cover.png', { type: 'image/png' })) => {
  fireEvent.change(screen.getByLabelText('选择封面图片'), { target: { files: [file] } })
  return file
}

it('previews a local file and uploads only after saving', async () => {
  vi.mocked(api.replaceCover).mockResolvedValue({ id: 'job', kind: 'cover', fileName: '', state: 'queued', reservedBytes: 0, createdAt: '' })
  vi.mocked(api.jobs).mockResolvedValue([{ id: 'job', kind: 'cover', fileName: '', state: 'succeeded', reservedBytes: 0, createdAt: '' }])
  setup()
  const file = choose()
  expect((await screen.findByAltText('新封面预览')).getAttribute('src')).toBe('blob:preview')
  expect(api.replaceCover).not.toHaveBeenCalled()
  expect(screen.queryByText('查找封面')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '保存封面' }))
  await screen.findByText('封面已保存')
  expect(api.replaceCover).toHaveBeenCalledExactlyOnceWith('song', file)
  expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview')
})
it('rejects oversized files and failed image previews without uploading', async () => {
  setup()
  const large = new File(['image'], 'large.png', { type: 'image/png' })
  Object.defineProperty(large, 'size', { value: 8 * 1024 * 1024 + 1 })
  choose(large)
  expect(screen.getByRole('alert').textContent).toContain('8 MiB')
  expect(screen.queryByRole('button', { name: '保存封面' })).toBeNull()
  choose()
  fireEvent.error(await screen.findByAltText('新封面预览'))
  expect((screen.getByRole('button', { name: '保存封面' }) as HTMLButtonElement).disabled).toBe(true)
  expect(api.replaceCover).not.toHaveBeenCalled()
})

it('reports processing failures and retains the chosen file for retry', async () => {
  vi.mocked(api.replaceCover).mockResolvedValue({ id: 'job', fileName: '', state: 'queued', reservedBytes: 0, createdAt: '' })
  vi.mocked(api.jobs).mockResolvedValue([{ id: 'job', fileName: '', state: 'failed', error: '存储空间不足', reservedBytes: 0, createdAt: '' }])
  const { busy } = setup()
  choose()
  fireEvent.click(screen.getByRole('button', { name: '保存封面' }))
  await screen.findByText('存储空间不足')
  await waitFor(() => expect((screen.getByRole('button', { name: '保存封面' }) as HTMLButtonElement).disabled).toBe(false))
  expect(busy).toHaveBeenCalledWith(true)
  expect(screen.getByAltText('新封面预览')).toBeTruthy()
})

it('releases preview object URLs when closed without saving', async () => {
  const { view } = setup()
  choose()
  await screen.findByAltText('新封面预览')
  view.unmount()
  expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview')
  expect(api.replaceCover).not.toHaveBeenCalled()
})
