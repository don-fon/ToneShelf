import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LyricsDisplaySettings } from './LyricsDisplaySettings'

afterEach(cleanup)

it('only offers timestamp grouping and independent lines without a pairing suggestion', () => {
  const lyrics = '[00:01]First\n[00:03]Uno\n[00:03]Second\n[00:05]Dos\n[00:05]Third\n[00:07]Tres'
  const onChange = vi.fn()
  const view = render(<LyricsDisplaySettings lyrics={lyrics} onChange={onChange}/>)
  expect(screen.queryByText(/句尾|可能存在/)).toBeNull()
  expect(screen.queryByLabelText('整体偏移（秒）')).toBeNull()
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.click(screen.getByText('歌词显示'))
  expect(screen.getByLabelText('分组方式')).toHaveProperty('value', 'timestamp')
  expect(screen.getByLabelText('分组方式').querySelectorAll('option')).toHaveLength(2)
  expect(screen.getByRole('region', { name: '歌词显示预览' }).querySelectorAll('p')).toHaveLength(4)
  fireEvent.change(screen.getByLabelText('主行'), { target: { value: 'last' } })
  expect(onChange).toHaveBeenLastCalledWith({ lyricsLayout: 'timestamp', lyricsPrimaryLine: 'last' })
  fireEvent.change(screen.getByLabelText('分组方式'), { target: { value: 'separate' } })
  expect(onChange).toHaveBeenLastCalledWith({ lyricsLayout: 'separate', lyricsPrimaryLine: 'first' })
  view.rerender(<LyricsDisplaySettings lyrics={lyrics} layout="separate" onChange={onChange}/>)
  expect(screen.getByLabelText('主行')).toHaveProperty('disabled', true)
  expect(screen.getByRole('region', { name: '歌词显示预览' }).querySelectorAll('p')).toHaveLength(6)
})
