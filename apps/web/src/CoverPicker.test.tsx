import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Track } from '@music/domain'
import { CoverPicker } from './CoverPicker'

beforeEach(() => { URL.createObjectURL = vi.fn(() => 'blob:preview'); URL.revokeObjectURL = vi.fn() })
afterEach(() => { cleanup(); vi.resetAllMocks() })
const track = { id: 'song', title: 'Song', artist: 'Singer', album: 'Album', coverUrl: '/covers/song' } as Track
function setup() {
  const changed = vi.fn(), invalid = vi.fn()
  function Editor() {
    const [file, setFile] = useState<File | null>(null)
    return <CoverPicker track={track} file={file} onChange={(next) => { changed(next); setFile(next) }} onInvalidChange={invalid}/>
  }
  return { changed, invalid, view: render(<Editor/>) }
}
const choose = (file = new File(['image'], 'cover.png', { type: 'image/png' })) => {
  fireEvent.change(screen.getByLabelText('选择封面图片'), { target: { files: [file] } })
  return file
}

it('only prepares a cover draft without separate save or cancel buttons', async () => {
  const { changed } = setup()
  const button = screen.getByRole('button', { name: '替换封面' })
  expect(button.querySelector('.settings-cover img')?.getAttribute('src')).toBe(track.coverUrl)
  const file = choose()
  const preview = await screen.findByAltText('新封面预览')
  expect(preview.getAttribute('src')).toBe('blob:preview')
  expect(button.contains(preview)).toBe(true)
  expect(document.querySelector('.cover-upload-preview')).toBeNull()
  expect(changed).toHaveBeenLastCalledWith(file)
  expect(screen.queryByRole('button', { name: '保存封面' })).toBeNull()
  expect(screen.queryByRole('button', { name: '取消替换封面' })).toBeNull()
})

it('rejects oversized files and allows reselecting after a failed preview', async () => {
  const { changed, invalid } = setup()
  const large = new File(['image'], 'large.png', { type: 'image/png' })
  Object.defineProperty(large, 'size', { value: 8 * 1024 * 1024 + 1 })
  choose(large)
  expect(screen.getByRole('alert').textContent).toContain('8 MiB')
  expect(changed).toHaveBeenLastCalledWith(null)
  choose()
  fireEvent.error(await screen.findByAltText('新封面预览'))
  expect(invalid).toHaveBeenLastCalledWith(true)
  choose(new File(['new image'], 'new.png', { type: 'image/png' }))
  expect(invalid).toHaveBeenLastCalledWith(false)
  expect(screen.queryByRole('alert')).toBeNull()
})

it('releases preview object URLs when closed without saving', async () => {
  const { view } = setup()
  choose()
  await screen.findByAltText('新封面预览')
  view.unmount()
  expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview')
})
