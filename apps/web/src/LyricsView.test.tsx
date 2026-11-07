import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LyricsView } from './LyricsView'

afterEach(cleanup)

describe('LyricsView', () => {
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
})
