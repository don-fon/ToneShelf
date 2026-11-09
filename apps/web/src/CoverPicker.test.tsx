import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { CoverPicker } from './CoverPicker'
import { api } from './api'

vi.mock('./api', () => ({ api: { searchCovers: vi.fn(), selectCover: vi.fn(), jobs: vi.fn().mockResolvedValue([]) } }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const track = { id: 'song', title: 'Song', artist: 'Singer', album: 'Album' } as Track
const candidate = { id: 'release', title: 'Release album', artist: 'Singer', score: 90, version: 'live', durationSeconds: 123 }
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const busy = vi.fn()
  render(<QueryClientProvider client={client}><CoverPicker track={track} onBusyChange={busy}/></QueryClientProvider>)
  fireEvent.click(screen.getByText('在线查找'))
  return { client, busy }
}

it('keeps search separate from metadata, previews candidates and saves only after confirmation', async () => {
  vi.mocked(api.searchCovers).mockResolvedValue({ queryTitle: 'Song', queryArtist: 'Singer', candidates: [candidate], expanded: false, partial: false })
  vi.mocked(api.selectCover).mockResolvedValue({ id: 'job', kind: 'cover', fileName: '', state: 'queued', reservedBytes: 0, createdAt: '' })
  vi.mocked(api.jobs).mockResolvedValue([{ id: 'job', kind: 'cover', fileName: '', state: 'succeeded', reservedBytes: 0, createdAt: '' }])
  setup()
  fireEvent.change(screen.getByLabelText('封面搜索歌名'), { target: { value: 'Japanese title' } })
  fireEvent.keyDown(screen.getByLabelText('封面搜索歌名'), { key: 'Enter' })
  fireEvent.click(await screen.findByRole('button', { name: /Release album/ }))
  expect(api.searchCovers).toHaveBeenCalledWith('song', { title: 'Japanese title', artist: 'Singer', album: 'Album' })
  expect(api.selectCover).not.toHaveBeenCalled()
  expect(screen.getByAltText('Release album封面').getAttribute('src')).toBe('/api/tracks/song/cover/candidates/release')
  expect(screen.getByText(/live/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '保存封面' }))
  await screen.findByText('封面已保存')
  expect(api.selectCover).toHaveBeenCalledExactlyOnceWith('song', 'release')
})

it('shows service errors, empty results and unavailable preview images', async () => {
  vi.mocked(api.searchCovers).mockRejectedValueOnce(new Error('HTTP 503')).mockResolvedValueOnce({ queryTitle: '', queryArtist: '', candidates: [], expanded: true, partial: false }).mockResolvedValueOnce({ queryTitle: '', queryArtist: '', candidates: [candidate], expanded: false, partial: true })
  setup()
  fireEvent.click(screen.getByRole('button', { name: '查找封面' }))
  expect((await screen.findByRole('alert')).textContent).toContain('503')
  fireEvent.click(screen.getByRole('button', { name: '查找封面' }))
  await screen.findByText(/没有找到可用封面/)
  fireEvent.click(screen.getByRole('button', { name: '查找封面' }))
  const image = await screen.findByAltText('Release album封面')
  fireEvent.error(image)
  expect(screen.getByText('不可用')).toBeTruthy()
  expect((screen.getByRole('button', { name: /Release album/ }) as HTMLButtonElement).disabled).toBe(true)
  expect(screen.queryByRole('button', { name: '保存封面' })).toBeNull()
})

it('reports background save failures and allows retrying without losing the selection', async () => {
  vi.mocked(api.searchCovers).mockResolvedValue({ queryTitle: '', queryArtist: '', candidates: [candidate], expanded: false, partial: false })
  vi.mocked(api.selectCover).mockResolvedValue({ id: 'job', fileName: '', state: 'queued', reservedBytes: 0, createdAt: '' })
  vi.mocked(api.jobs).mockResolvedValue([{ id: 'job', fileName: '', state: 'failed', error: '存储空间不足', reservedBytes: 0, createdAt: '' }])
  const { busy } = setup()
  fireEvent.click(screen.getByRole('button', { name: '查找封面' }))
  fireEvent.click(await screen.findByRole('button', { name: /Release album/ }))
  fireEvent.click(screen.getByRole('button', { name: '保存封面' }))
  await screen.findByText('存储空间不足')
  await waitFor(() => expect((screen.getByRole('button', { name: '保存封面' }) as HTMLButtonElement).disabled).toBe(false))
  expect(busy).toHaveBeenCalledWith(true)
})
