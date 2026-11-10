import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { FloatingLyrics } from './FloatingLyrics'

const frames: HTMLIFrameElement[] = []
afterEach(() => { cleanup(); frames.forEach((f) => f.remove()); frames.length = 0; vi.unstubAllGlobals() })
const track = { id: 'song', title: 'Song', artist: 'Singer', lyrics: '[kana:1やま1かわ]\n[00:01]山\n[00:01]Mountain\n[00:03]川\n[00:03]River', lyricsLayout: 'timestamp', lyricsPrimaryLine: 'first' } as Track
const props = () => ({ track, position: 0, playing: false, onToggle: vi.fn(), onNext: vi.fn() })
function setupAPI() {
  vi.stubGlobal('isSecureContext', true)
  const windows: Array<{ window: Window; close: ReturnType<typeof vi.fn> }> = []
  const requestWindow = vi.fn(async () => {
    const frame = document.createElement('iframe'); document.body.append(frame); frames.push(frame)
    const window = frame.contentWindow!
    Object.defineProperty(window, 'closed', { configurable: true, value: false })
    const close = vi.fn(() => { Object.defineProperty(window, 'closed', { configurable: true, value: true }); window.dispatchEvent(new Event('pagehide')) })
    window.close = close
    windows.push({ window, close })
    return window
  })
  vi.stubGlobal('documentPictureInPicture', { requestWindow })
  return { requestWindow, windows }
}

it('shows a clear unsupported-browser error and does not open an ordinary popup', async () => {
  vi.stubGlobal('isSecureContext', true)
  vi.stubGlobal('documentPictureInPicture', undefined)
  const popup = vi.spyOn(window, 'open')
  render(<FloatingLyrics {...props()}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  expect((await screen.findByRole('alert')).textContent).toContain('当前浏览器不支持')
  expect(popup).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '关闭悬浮歌词提示' }))
  expect(screen.queryByRole('alert')).toBeNull()
})

it('requires a selected track and reports insecure connections', async () => {
  const { requestWindow } = setupAPI()
  const view = render(<FloatingLyrics {...props()} track={null}/> )
  expect((screen.getByRole('button', { name: '悬浮歌词' }) as HTMLButtonElement).disabled).toBe(true)
  view.rerender(<FloatingLyrics {...props()}/> )
  vi.stubGlobal('isSecureContext', false)
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  expect((await screen.findByRole('alert')).textContent).toContain('HTTPS')
  expect(requestWindow).not.toHaveBeenCalled()
})

it('updates the current bilingual group and kana, follows song changes and shares playback controls', async () => {
  const { windows, requestWindow } = setupAPI()
  const controls = props()
  const view = render(<FloatingLyrics {...controls} position={0.699}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '关闭悬浮歌词' })).toBeTruthy())
  const pip = within(windows[0].window.document.body)
  expect(pip.getByText('等待歌词…')).toBeTruthy()
  view.rerender(<FloatingLyrics {...controls} position={0.7}/> )
  expect(pip.getByText('Mountain')).toBeTruthy()
  expect(pip.getByText('River').closest('p')?.className).toBe('next')
  expect(pip.getByText('Mountain').closest('p')?.getAttribute('aria-current')).toBe('true')
  expect(pip.getByText('やま').tagName).toBe('RT')
  view.rerender(<FloatingLyrics {...controls} position={2.7} playing/> )
  expect(pip.getByText('River')).toBeTruthy()
  expect(pip.getByText('かわ').tagName).toBe('RT')
  expect(pip.queryByText('Mountain')).toBeNull()
  fireEvent.click(pip.getByRole('button', { name: '暂停' }))
  fireEvent.click(pip.getByRole('button', { name: '下一首' }))
  expect(controls.onToggle).toHaveBeenCalledOnce()
  expect(controls.onNext).toHaveBeenCalledOnce()
  view.rerender(<FloatingLyrics {...controls} track={{ ...track, id: 'other', title: 'Other', lyrics: '' }}/> )
  expect(pip.queryByRole('heading')).toBeNull()
  expect(pip.getByText('暂无歌词')).toBeTruthy()
  expect(requestWindow).toHaveBeenCalledOnce()
  expect(windows[0].window.document.querySelectorAll('audio,video')).toHaveLength(0)
})

it('recovers from a rejected request and cleans up after closing or losing the current song', async () => {
  const { windows, requestWindow } = setupAPI()
  requestWindow.mockRejectedValueOnce(new Error('打开被拒绝'))
  const view = render(<FloatingLyrics {...props()}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  expect((await screen.findByRole('alert')).textContent).toContain('打开被拒绝')
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  fireEvent.click(screen.getByRole('button', { name: '关闭悬浮歌词' }))
  await screen.findByRole('button', { name: '悬浮歌词' })
  expect(windows[0].close).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  view.rerender(<FloatingLyrics {...props()} track={null}/> )
  await waitFor(() => expect(windows[1].close).toHaveBeenCalledOnce())
  expect(requestWindow).toHaveBeenCalledTimes(3)
})

it('shows plain lyrics and closes the window when the component unmounts', async () => {
  const { windows } = setupAPI()
  const view = render(<FloatingLyrics {...props()} track={{ ...track, lyrics: 'Plain first\nPlain second' }}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  const pip = within(windows[0].window.document.body)
  expect(pip.getByText('Plain first')).toBeTruthy()
  expect(pip.getByText('Plain second')).toBeTruthy()
  view.unmount()
  expect(windows[0].close).toHaveBeenCalledOnce()
})

it('stops retaining controls for focus after pointer use and restores keyboard access without blurring', async () => {
  const { windows } = setupAPI()
  render(<FloatingLyrics {...props()}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  const doc = windows[0].window.document
  const player = doc.querySelector('.pip-player')!
  const button = within(doc.body).getByRole('button', { name: '播放' })
  button.focus()
  fireEvent.keyDown(button, { key: 'Tab' })
  expect(player.classList.contains('keyboard-controls')).toBe(true)
  fireEvent.pointerMove(player)
  expect(player.classList.contains('keyboard-controls')).toBe(false)
  expect(doc.activeElement).toBe(button)
  fireEvent.keyDown(button, { key: ' ' })
  expect(player.classList.contains('keyboard-controls')).toBe(true)
  fireEvent.pointerLeave(player)
  expect(player.classList.contains('keyboard-controls')).toBe(false)
  expect(doc.activeElement).toBe(button)
  fireEvent.keyDown(button, { key: 'Tab' })
  fireEvent.pointerDown(button)
  expect(player.classList.contains('keyboard-controls')).toBe(false)
})

it('applies offset and shows all simultaneous lines in independent-line mode', async () => {
  const { windows } = setupAPI()
  const settings = { ...track, lyrics: '[offset:500]\n[00:01]First\n[00:01]Second', lyricsLayout: 'separate' as const }
  const view = render(<FloatingLyrics {...props()} track={settings} position={.199}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  const pip = within(windows[0].window.document.body)
  expect(pip.getByText('等待歌词…')).toBeTruthy()
  view.rerender(<FloatingLyrics {...props()} track={settings} position={.2}/> )
  expect(pip.getByText('First')).toBeTruthy()
  expect(pip.getByText('Second')).toBeTruthy()
})

it('closes a window that finishes opening after the component unmounts', async () => {
  const { windows, requestWindow } = setupAPI()
  const createWindow = requestWindow.getMockImplementation()!
  let complete!: (window: Window) => void
  requestWindow.mockImplementationOnce(() => new Promise((resolve) => { complete = resolve }))
  const view = render(<FloatingLyrics {...props()}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  view.unmount()
  await act(async () => { complete(await createWindow()) })
  expect(windows[0].close).toHaveBeenCalledOnce()
})

it('previews the next complete time group and follows cover changes with an image-failure fallback', async () => {
  const { windows } = setupAPI()
  const song = { ...track, coverUrl: '/cover-a.jpg', lyricsLayout: 'separate' as const, lyrics: '[00:01]Current\n[00:03]Next original\n[00:03]Next translation\n[00:05]Later' }
  const view = render(<FloatingLyrics {...props()} track={song} position={1}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  const doc = windows[0].window.document
  const pip = within(doc.body)
  expect(pip.getByText('Current').closest('p')?.className).toBe('current')
  expect(pip.getByText('Next original').closest('p')?.className).toBe('next')
  expect(pip.getByText('Next translation').closest('p')?.className).toBe('next')
  expect(pip.queryByText('Later')).toBeNull()
  expect(doc.querySelector('.pip-cover')?.getAttribute('src')).toBe('/cover-a.jpg')
  fireEvent.error(doc.querySelector('.pip-cover')!)
  expect(doc.querySelector('.pip-cover')).toBeNull()
  view.rerender(<FloatingLyrics {...props()} track={{ ...song, title: 'Updated', coverUrl: '/cover-b.jpg' }} position={5}/> )
  expect(doc.querySelector('.pip-cover')?.getAttribute('src')).toBe('/cover-b.jpg')
  expect(pip.getByText('Later').closest('p')?.className).toBe('current')
  expect(doc.querySelector('.next')).toBeNull()
})

it('animates adjacent lyric groups without restarting on progress updates and cancels on seeks or song changes', async () => {
  const { windows } = setupAPI()
  const song = { ...track, lyrics: '[00:01]First\n[00:03]Second\n[00:05]Third\n[00:07]Fourth', lyricsLayout: 'separate' as const }
  const view = render(<FloatingLyrics {...props()} track={song} position={1}/> )
  fireEvent.click(screen.getByRole('button', { name: '悬浮歌词' }))
  await screen.findByRole('button', { name: '关闭悬浮歌词' })
  const doc = windows[0].window.document
  const animations: Array<{ cancel: ReturnType<typeof vi.fn>; onfinish: (() => void) | null }> = []
  const animate = vi.fn(() => { const animation = { cancel: vi.fn(), onfinish: null }; animations.push(animation); return animation })
  Object.defineProperty(doc.defaultView!.Element.prototype, 'animate', { configurable: true, value: animate })
  const reducedMotion = vi.fn(() => ({ matches: false }))
  Object.defineProperty(doc.defaultView!, 'matchMedia', { configurable: true, value: reducedMotion })
  view.rerender(<FloatingLyrics {...props()} track={song} position={3}/> )
  expect(animate).toHaveBeenCalledTimes(3)
  expect(doc.querySelector('.current')?.textContent).toBe('Second')
  expect(doc.querySelector('.next')?.textContent).toBe('Third')
  expect(doc.querySelector('.pip-exiting')?.getAttribute('aria-hidden')).toBe('true')
  animations.at(-1)!.onfinish!()
  expect(doc.querySelector('.pip-exiting')).toBeNull()
  view.rerender(<FloatingLyrics {...props()} track={song} position={3.2}/> )
  expect(animate).toHaveBeenCalledTimes(3)
  view.rerender(<FloatingLyrics {...props()} track={song} position={7}/> )
  expect(animations.every((animation) => animation.cancel.mock.calls.length === 1)).toBe(true)
  expect(animate).toHaveBeenCalledTimes(3)
  view.rerender(<FloatingLyrics {...props()} track={song} position={1}/> )
  reducedMotion.mockReturnValue({ matches: true })
  view.rerender(<FloatingLyrics {...props()} track={song} position={3}/> )
  expect(animate).toHaveBeenCalledTimes(3)
  reducedMotion.mockReturnValue({ matches: false })
  view.rerender(<FloatingLyrics {...props()} track={{ ...song, id: 'other' }} position={5}/> )
  expect(animate).toHaveBeenCalledTimes(3)
  view.unmount()
})
