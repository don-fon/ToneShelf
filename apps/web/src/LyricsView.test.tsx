import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LyricsView } from './LyricsView'

afterEach(cleanup)

describe('LyricsView', () => {
  it('highlights and scrolls 300ms early while keeping lyric seeks on their timestamps', () => {
    const onSeek = vi.fn()
    const lyrics = '[00:01]First\n[00:03]Second'
    const view = render(<LyricsView trackTitle="song" lyrics={lyrics} position={0.699} onSeek={onSeek}/>)
    expect(screen.queryByRole('button', { current: true })).toBeNull()
    const first = screen.getByRole('button', { name: 'First' })
    const second = screen.getByRole('button', { name: 'Second' })
    const container = view.container.querySelector<HTMLDivElement>('.lyrics-scroll')!
    Object.defineProperty(container, 'clientHeight', { value: 200 })
    container.scrollTo = vi.fn()
    vi.spyOn(container, 'getBoundingClientRect').mockReturnValue({ top: 100 } as DOMRect)
    vi.spyOn(first, 'getBoundingClientRect').mockReturnValue({ top: 150, height: 20 } as DOMRect)
    vi.spyOn(second, 'getBoundingClientRect').mockReturnValue({ top: 400, height: 20 } as DOMRect)
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} position={0.7} onSeek={onSeek}/>)
    expect(first.getAttribute('aria-current')).toBe('true')
    expect(container.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' })
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} position={2.699} onSeek={onSeek}/>)
    expect(first.getAttribute('aria-current')).toBe('true')
    expect(container.scrollTo).toHaveBeenCalledTimes(1)
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} position={2.7} onSeek={onSeek}/>)
    expect(second.getAttribute('aria-current')).toBe('true')
    expect(container.scrollTo).toHaveBeenLastCalledWith({ top: 210, behavior: 'smooth' })
    fireEvent.click(second)
    expect(onSeek).toHaveBeenCalledWith(3)
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} position={1} onSeek={onSeek}/>)
    expect(first.getAttribute('aria-current')).toBe('true')
    expect(container.scrollTo).toHaveBeenCalledTimes(3)
  })

  it.each([[1000, 1.7, 2], [-1000, 3.7, 4]])('adds animation lead to offset %i without changing seek position', (offset, boundary, target) => {
    const onSeek = vi.fn()
    const lyrics = `[offset:${offset}]\n[00:00]First\n[00:03]Second`
    const view = render(<LyricsView trackTitle="song" lyrics={lyrics} position={boundary - 0.001} onSeek={onSeek}/>)
    const second = screen.getByRole('button', { name: 'Second' })
    expect(second.hasAttribute('aria-current')).toBe(false)
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} position={boundary} onSeek={onSeek}/>)
    expect(second.getAttribute('aria-current')).toBe('true')
    fireEvent.click(second)
    expect(onSeek).toHaveBeenCalledWith(target)
  })

  it('keeps uniquely aligned kana visible on either the primary or secondary line', () => {
    const lyrics = '[kana:1やま1かわ]\n[00:01]高山\n[00:01]山\n[00:03]河流\n[00:03]川\n[00:05]尾注'
    const onSeek = vi.fn()
    const view = render(<LyricsView trackTitle="song" lyrics={lyrics} position={1} onSeek={onSeek}/>)
    expect(view.container.querySelector('.lyric-original rt')).toBeNull()
    expect(view.container.querySelector('.lyric-secondary rt')?.textContent).toBe('やま')
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} primaryLine="last" position={1} onSeek={onSeek}/>)
    expect(view.container.querySelector('.lyric-original rt')?.textContent).toBe('やま')
    expect(view.container.querySelector('.lyric-secondary rt')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '川 河流' }))
    expect(onSeek).toHaveBeenCalledWith(3)
  })

  it('renders kana above the corresponding text and preserves seeking and group selection', () => {
    const onSeek = vi.fn()
    const lyrics = '[kana:1やま1かわ]\n[00:01]山と川\n[00:01]Mountain and river'
    const view = render(<LyricsView trackTitle="song" lyrics={lyrics} position={1} onSeek={onSeek}/>)
    expect([...view.container.querySelectorAll('ruby')].map((ruby) => [ruby.firstChild?.textContent, ruby.querySelector('rt')?.textContent])).toEqual([
      ['山', 'やま'], ['川', 'かわ'],
    ])
    fireEvent.click(screen.getByRole('button', { name: '山と川 Mountain and river' }))
    expect(onSeek).toHaveBeenCalledWith(1)
    view.rerender(<LyricsView trackTitle="song" lyrics={lyrics} primaryLine="last" position={1} onSeek={onSeek}/>)
    expect(view.container.querySelector('.lyric-original ruby')).toBeNull()
    expect(view.container.querySelectorAll('.lyric-secondary rt')).toHaveLength(2)
  })

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
