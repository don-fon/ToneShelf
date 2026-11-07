import { describe, expect, it } from 'vitest'
import { activeLyricIndex, parseLyrics } from './lyrics'

describe('lyrics playback', () => {
  it('parses timestamps, metadata, multiple timestamps and offset', () => {
    const parsed = parseLyrics('\uFEFF[ar:artist]\n[00:01.20][00:03.2]line one\n[offset:500]\n[00:04]line two')

    expect(parsed.synced).toBe(true)
    expect(parsed.lines.map((line) => [line.timeSeconds, line.text])).toEqual([
      [1.7, 'line one'], [3.7, 'line one'], [4.5, 'line two'],
    ])
  })

  it('keeps ordinary lyrics as unsynchronized lines', () => {
    const parsed = parseLyrics('first\n\nsecond')
    expect(parsed.synced).toBe(false)
    expect(parsed.lines.map((line) => line.text)).toEqual(['first', 'second'])
  })

  it('finds the active line', () => {
    const lines = parseLyrics('[00:01]first\n[00:03]second').lines
    expect(activeLyricIndex(lines, 0.9)).toBe(-1)
    expect(activeLyricIndex(lines, 1)).toBe(0)
    expect(activeLyricIndex(lines, 3.5)).toBe(1)
  })
})
