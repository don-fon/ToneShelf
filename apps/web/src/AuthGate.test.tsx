import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { AuthGate } from './AuthGate'
import { api, APIError } from './api'
import { initializePlayerSession } from './player-store'
import { notifyAuthExpired } from './platform/auth-events'

vi.mock('./App', () => ({ App: ({ onLogout }: { onLogout: () => void }) => <div>受保护音乐库<button onClick={onLogout}>退出登录</button></div> }))
vi.mock('./player-store', () => ({ initializePlayerSession: vi.fn(() => vi.fn()) }))
vi.mock('./api', () => ({
  APIError: class extends Error { constructor(message: string, readonly status: number) { super(message) } },
  api: { currentUser: vi.fn(), login: vi.fn(), logout: vi.fn() },
}))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const first = { id: 'first', isDefault: true }
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['playlists'], ['old private content'])
  render(<QueryClientProvider client={client}><AuthGate/></QueryClientProvider>)
  return client
}

it('requires login and uses a visible account field with no registration or password field', async () => {
  vi.mocked(api.currentUser).mockRejectedValue(new APIError('请先登录', 401))
  vi.mocked(api.login).mockRejectedValueOnce(new APIError('账号无效', 401)).mockResolvedValueOnce(first)
  setup()
  const input = await screen.findByRole('textbox', { name: '账号' })
  expect((input as HTMLInputElement).type).toBe('text')
  expect(screen.queryByText('注册')).toBeNull()
  expect(screen.queryByText('受保护音乐库')).toBeNull()
  fireEvent.change(input, { target: { value: 'wrong' } }); fireEvent.click(screen.getByRole('button', { name: '登录' }))
  expect((await screen.findByRole('alert')).textContent).toContain('账号无效')
  fireEvent.change(input, { target: { value: 'provided account' } }); fireEvent.click(screen.getByRole('button', { name: '登录' }))
  await screen.findByText('受保护音乐库')
  expect(api.login).toHaveBeenLastCalledWith('provided account')
  expect(initializePlayerSession).toHaveBeenCalledWith(first)
  expect(screen.queryByRole('textbox')).toBeNull()
})

it('restores identity before playback and clears private queries on logout or a 401', async () => {
  vi.mocked(api.currentUser).mockResolvedValue(first)
  vi.mocked(api.logout).mockResolvedValue(undefined)
  const client = setup()
  await screen.findByText('受保护音乐库')
  fireEvent.click(screen.getByRole('button', { name: '退出登录' }))
  await screen.findByRole('textbox', { name: '账号' })
  expect(client.getQueryCache().getAll()).toHaveLength(0)
  vi.mocked(api.login).mockResolvedValue({ id: 'second', isDefault: false })
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'second account' } }); fireEvent.click(screen.getByRole('button', { name: '登录' }))
  await screen.findByText('受保护音乐库')
  expect(initializePlayerSession).toHaveBeenLastCalledWith({ id: 'second', isDefault: false })
  act(() => { notifyAuthExpired() })
  expect(screen.queryByText('受保护音乐库')).toBeNull()
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('')
})

it('keeps the session visible if logout fails and does not interpret network errors as login success', async () => {
  vi.mocked(api.currentUser).mockRejectedValueOnce(new Error('offline'))
  const client = setup()
  expect((await screen.findByRole('alert')).textContent).toContain('无法连接服务器')
  expect(screen.queryByText('受保护音乐库')).toBeNull()
  vi.mocked(api.login).mockResolvedValue(first)
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'account' } }); fireEvent.click(screen.getByRole('button', { name: '登录' }))
  await screen.findByText('受保护音乐库')
  vi.mocked(api.logout).mockRejectedValueOnce(new Error('退出失败'))
  fireEvent.click(screen.getByRole('button', { name: '退出登录' }))
  expect((await screen.findByRole('alert')).textContent).toContain('退出失败')
  expect(screen.getByText('受保护音乐库')).toBeTruthy()
  await waitFor(() => expect(client.getQueryCache().getAll()).toHaveLength(1))
})

it('rechecks identity on focus and clears private queries before switching users', async () => {
  vi.mocked(api.currentUser).mockResolvedValue(first)
  const client = setup()
  await screen.findByText('受保护音乐库')
  vi.mocked(api.currentUser).mockResolvedValue({ id: 'second', isDefault: false })
  fireEvent.focus(window)
  await waitFor(() => expect(initializePlayerSession).toHaveBeenLastCalledWith({ id: 'second', isDefault: false }))
  expect(client.getQueryCache().getAll()).toHaveLength(0)
})
