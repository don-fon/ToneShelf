import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LyricsEditor } from './LyricsEditor'

afterEach(cleanup)

it('supports keyboard navigation between the shared editing and display panels', () => {
  render(<LyricsEditor lyrics="[00:01]原文\n[00:01]译文" onTextChange={vi.fn()} onDisplayChange={vi.fn()}/> )
  const edit = screen.getByRole('tab', { name: '编辑' })
  const display = screen.getByRole('tab', { name: '显示设置' })
  expect(screen.getByRole('textbox', { name: '歌词' })).toBeTruthy()
  expect(screen.queryByRole('region', { name: '歌词显示预览' })).toBeNull()
  fireEvent.keyDown(edit, { key: 'ArrowRight' })
  expect(document.activeElement).toBe(display)
  expect(display.getAttribute('aria-selected')).toBe('true')
  expect(screen.queryByRole('textbox', { name: '歌词' })).toBeNull()
  expect(screen.getByRole('region', { name: '歌词显示预览' }).textContent).toContain('译文')
  fireEvent.keyDown(display, { key: 'Home' })
  expect(document.activeElement).toBe(edit)
  expect(edit.getAttribute('aria-selected')).toBe('true')
})
