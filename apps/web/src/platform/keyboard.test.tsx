import { cleanup, fireEvent, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { usePlaybackShortcuts } from './keyboard'

afterEach(() => { cleanup(); document.body.replaceChildren() })
const actions = () => ({ enabled: true, togglePlayback: vi.fn(), next: vi.fn(), seekBy: vi.fn(), adjustVolume: vi.fn(), toggleMute: vi.fn(), showHelp: vi.fn() })

it('handles playback keys without scrolling, ignores held toggle keys, and allows repeated arrows', () => {
  const controls = actions()
  renderHook(() => usePlaybackShortcuts(controls))
  expect(fireEvent.keyDown(document.body, { key: ' ' })).toBe(false)
  fireEvent.keyDown(document.body, { key: ' ', repeat: true })
  expect(controls.togglePlayback).toHaveBeenCalledOnce()
  fireEvent.keyDown(document.body, { key: 'n' })
  fireEvent.keyDown(document.body, { key: 'n', repeat: true })
  expect(controls.next).toHaveBeenCalledOnce()
  fireEvent.keyDown(document.body, { key: 'ArrowLeft' })
  fireEvent.keyDown(document.body, { key: 'ArrowRight', repeat: true })
  expect(controls.seekBy.mock.calls).toEqual([[-5], [5]])
  fireEvent.keyDown(document.body, { key: 'ArrowUp' })
  fireEvent.keyDown(document.body, { key: 'ArrowDown' })
  expect(controls.adjustVolume.mock.calls).toEqual([[0.05], [-0.05]])
  fireEvent.keyDown(document.body, { key: 'm' })
  expect(controls.toggleMute).toHaveBeenCalledOnce()
  fireEvent.keyDown(document.body, { key: '?', shiftKey: true })
  expect(controls.showHelp).toHaveBeenCalledOnce()
})

it('leaves typing, sliders, native button activation and browser shortcuts alone', () => {
  const controls = actions()
  renderHook(() => usePlaybackShortcuts(controls))
  for (const html of ['<input>', '<textarea></textarea>', '<select></select>', '<div contenteditable="true"><span>text</span></div>', '<input type="range">']) {
    const container = document.createElement('div'); container.innerHTML = html; document.body.append(container)
    const target = container.querySelector('span') ?? container.firstElementChild!
    expect(fireEvent.keyDown(target, { key: ' ' })).toBe(true)
    expect(fireEvent.keyDown(target, { key: 'n' })).toBe(true)
    expect(fireEvent.keyDown(target, { key: 'ArrowRight' })).toBe(true)
    container.remove()
  }
  const button = document.createElement('button'); document.body.append(button)
  expect(fireEvent.keyDown(button, { key: ' ' })).toBe(true)
  for (const modifier of ['ctrlKey', 'metaKey', 'altKey', 'shiftKey']) fireEvent.keyDown(document.body, { key: 'n', [modifier]: true })
  fireEvent.keyDown(document.body, { key: 'n', isComposing: true })
  const consumed = new KeyboardEvent('keydown', { key: 'n', bubbles: true, cancelable: true }); consumed.preventDefault(); document.body.dispatchEvent(consumed)
  expect(controls.togglePlayback).not.toHaveBeenCalled()
  expect(controls.next).not.toHaveBeenCalled()
  expect(controls.seekBy).not.toHaveBeenCalled()
})

it('blocks global playback while a modal is open and uses the latest callbacks after rerendering', () => {
  const controls = actions()
  const hook = renderHook((value) => usePlaybackShortcuts(value), { initialProps: controls })
  const modal = document.createElement('div'); modal.className = 'modal'; document.body.append(modal)
  fireEvent.keyDown(document.body, { key: 'n' })
  expect(controls.next).not.toHaveBeenCalled()
  modal.remove()
  const updated = actions()
  hook.rerender(updated)
  fireEvent.keyDown(document.body, { key: 'n' })
  expect(updated.next).toHaveBeenCalledOnce()
  expect(controls.next).not.toHaveBeenCalled()
  hook.rerender({ ...updated, enabled: false })
  fireEvent.keyDown(document.body, { key: 'n' })
  expect(updated.next).toHaveBeenCalledOnce()
})
