import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { parseLyrics } from '@music/player-policy'
import { LyricsPreview } from './LyricsPreview'

afterEach(cleanup)

it('renders the same kana annotations in the settings preview', () => {
  const view = render(<LyricsPreview parsed={parseLyrics('[kana:1やま]\n[00:01]山')}/>)
  expect(view.container.querySelector('ruby')?.firstChild?.textContent).toBe('山')
  expect(view.container.querySelector('rt')?.textContent).toBe('やま')
})

it('shows milliseconds, minute boundaries and negative preview times correctly', () => {
  const view = render(<LyricsPreview parsed={parseLyrics('[00:59.999]Before\n[01:00]After')}/>)
  expect(screen.getByText('0:59.999')).toBeTruthy()
  expect(screen.getByText('1:00.000')).toBeTruthy()
  view.rerender(<LyricsPreview parsed={parseLyrics('[offset:1000]\n[00:00.001]Advanced')}/>)
  expect(screen.getByText('-0:00.999')).toBeTruthy()
})
