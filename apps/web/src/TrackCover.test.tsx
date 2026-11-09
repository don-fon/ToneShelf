import { cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { TrackCover } from './TrackCover'
import { LyricsView } from './LyricsView'

afterEach(cleanup)

it('falls back on failed images and retries after audio replacement', () => {
  const view = render(<TrackCover src="/old.jpg"/>)
  fireEvent.error(view.container.querySelector('img')!)
  expect(view.container.textContent).toBe('♫')
  view.rerender(<TrackCover src="/new.jpg"/>)
  expect(view.container.querySelector('img')?.getAttribute('src')).toBe('/new.jpg')
  view.rerender(<TrackCover/>)
  expect(view.container.querySelector('img')).toBeNull()
})

it('keeps lyrics clickable above a decorative cover, including without lyrics', () => {
  let sought = -1
  const view = render(<LyricsView coverURL="/art.jpg" trackTitle="song" lyrics="[00:01]Hello" position={1} onSeek={(seconds) => { sought = seconds }}/>)
  expect(view.container.querySelector('.lyrics-cover-background')?.getAttribute('aria-hidden')).toBe('true')
  fireEvent.click(view.getByRole('button', { name: 'Hello' }))
  expect(sought).toBe(1)
  view.rerender(<LyricsView coverURL="/art.jpg" trackTitle="song" lyrics="" position={1} onSeek={() => {}}/>)
  expect(view.getByText(/暂无歌词/)).toBeTruthy()
  fireEvent.error(view.container.querySelector('img')!)
  expect(view.container.querySelector('img')).toBeNull()
})
