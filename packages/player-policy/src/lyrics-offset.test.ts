import { describe, expect, it } from 'vitest'
import { activeLyricIndex, lyricsOffsetSeconds, parseLyrics } from './lyrics'

describe('manually edited lyrics offset', () => {
  it('delays or advances every timestamp and keeps bilingual lines together', () => {
    const delayed = '[offset:-1250]\n[00:01][00:03]Original\n[00:01]Translation'
    expect(lyricsOffsetSeconds(delayed)).toBe(1.25)
    expect(parseLyrics(delayed).lines.map((line) => line.timeSeconds)).toEqual([2.25, 4.25])
    expect(parseLyrics(delayed).lines[0].secondaryTexts).toEqual(['Translation'])
    const advanced = '[offset:2000]\n[00:01]First\n[00:03]Second'
    expect(parseLyrics(advanced).lines.map((line) => line.timeSeconds)).toEqual([-1, 1])
  })

  it('reads the last valid integer tag and ignores unsafe imported values', () => {
    expect(lyricsOffsetSeconds(' [OFFSET:+500] \r[offset:-1000]\r[00:04]line')).toBe(1)
    expect(lyricsOffsetSeconds('[offset:500]\n[00:04]line')).toBe(-.5)
    expect(lyricsOffsetSeconds('[offset:9007199254740992]\n[00:01]line')).toBe(0)
    expect(lyricsOffsetSeconds('[offset:0]\n[00:01]line')).toBe(0)
    expect(lyricsOffsetSeconds('[00:01]line')).toBe(0)
  })

  it('does not merge distinct lines moved before the start of the audio', () => {
    const parsed = parseLyrics('[offset:3000]\n[00:01]first\n[00:02]second\n[00:04]third')
    expect(parsed.lines.map((line) => [line.timeSeconds, line.text])).toEqual([[-2, 'first'], [-1, 'second'], [1, 'third']])
    expect(activeLyricIndex(parsed.lines, 0)).toBe(1)
  })
})
