import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LyricsView } from './LyricsView'

afterEach(cleanup)

describe('LyricsView', () => {
  it('follows overall offset and clamps seeking before audio start to zero', () => {
    const onSeek = vi.fn()
    const view = render(<LyricsView trackTitle="song" lyrics={'[offset:-1000]\n[00:01]First\n[00:03]Second'} position={1} onSeek={onSeek}/>)
    expect(screen.queryByRole('button', { current: true })).toBeNull()
    view.rerender(<LyricsView trackTitle="song" lyrics={'[offset:2000]\n[00:01]First\n[00:03]Second'} position={0} onSeek={onSeek}/>)
    const first = screen.getByRole('button', { name: 'First' })
    expect(first.getAttribute('aria-current')).toBe('true')
    fireEvent.click(first)
    expect(onSeek).toHaveBeenCalledWith(0)
  })

  it('highlights the active LRC line and seeks when a line is selected', () => {
    const onSeek = vi.fn()
    const view = render(<LyricsView trackTitle="song" lyrics={'[00:01]first\n[00:03]second'} position={2} onSeek={onSeek}/>)

    expect(screen.getByRole('button', { name: 'first' }).getAttribute('aria-current')).toBe('true')
    view.rerender(<LyricsView trackTitle="song" lyrics={'[00:01]first\n[00:03]second'} position={4} onSeek={onSeek}/>)
    expect(screen.getByRole('button', { name: 'second' }).getAttribute('aria-current')).toBe('true')
    fireEvent.click(screen.getByRole('button', { name: 'second' }))
    expect(onSeek).toHaveBeenCalledWith(3)
  })

  it('renders plain lyrics without seek buttons', () => {
    render(<LyricsView trackTitle="song" lyrics={'first\nsecond'} position={10} onSeek={vi.fn()}/>)
    expect(screen.getByText('first')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('highlights and seeks a simultaneous bilingual group as one unit', () => {
    const onSeek = vi.fn()
    render(<LyricsView trackTitle="song" lyrics={'[00:01]Hello\n[00:01]你好\n[00:03]Next'} position={2} onSeek={onSeek}/>)
    const group = screen.getByRole('button', { name: 'Hello 你好' })
    expect(group.getAttribute('aria-current')).toBe('true')
    expect(screen.getByText('你好').className).toBe('lyric-secondary')
    expect(screen.getAllByRole('button')).toHaveLength(2)
    fireEvent.click(screen.getByText('你好'))
    expect(onSeek).toHaveBeenCalledWith(1)
  })

  it('does not pair adjacent lines whose original timestamps differ', () => {
    render(<LyricsView trackTitle="song" lyrics={'[00:01]First\n[00:04]Uno\n[00:04]Second\n[00:07]Dos'} position={1} onSeek={vi.fn()}/>)
    expect(screen.getByRole('button', { name: 'First' }).getAttribute('aria-current')).toBe('true')
    expect(screen.getByRole('button', { name: 'Uno Second' }).hasAttribute('aria-current')).toBe(false)
  })

  it('highlights all simultaneous independent lines and allows selecting either one', () => {
    const onSeek = vi.fn()
    render(<LyricsView trackTitle="song" lyrics={'[00:01]First\n[00:01]Second'} layout="separate" position={1} onSeek={onSeek}/>)
    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(2)
    expect(buttons.every((button) => button.getAttribute('aria-current') === 'true')).toBe(true)
    fireEvent.click(buttons[0])
    expect(onSeek).toHaveBeenCalledWith(1)
  })
})
