import { describe, expect, it } from 'vitest'
import { activeLyricIndex, parseLyrics } from './lyrics'

describe('lyrics playback', () => {
  it('parses timestamps, metadata, multiple timestamps and offset', () => {
    const parsed = parseLyrics('\uFEFF[ar:artist]\n[00:01.20][00:03.2]line one\n[offset:500]\n[00:04]line two')

    expect(parsed.synced).toBe(true)
    expect(parsed.lines.map((line) => [line.timeSeconds, line.text])).toEqual([
      [.7, 'line one'], [2.7, 'line one'], [3.5, 'line two'],
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

  it('groups simultaneous bilingual lines and removes duplicate text', () => {
    const parsed = parseLyrics('[00:01]Hello\n[00:01]你好\n[00:01]你好\n[00:01]こんにちは\n[00:03]Next')
    expect(parsed.lines.map(({ timeSeconds, text, secondaryTexts }) => [timeSeconds, text, secondaryTexts])).toEqual([
      [1, 'Hello', ['你好', 'こんにちは']], [3, 'Next', undefined],
    ])
    expect(activeLyricIndex(parsed.lines, 1)).toBe(0)
    expect(activeLyricIndex(parsed.lines, 2.9)).toBe(0)
    expect(activeLyricIndex(parsed.lines, 3)).toBe(1)
  })

  it('keeps ordinary same-time Japanese/Chinese pairs at their own timestamps', () => {
    const parsed = parseLyrics('[00:01]君と歩いた\n[00:01]和你一起走过\n[00:03]空を見上げた\n[00:03]抬头望向天空\n[00:05]手をつないだ\n[00:05]牵起了你的手')
    expect(parsed.lines.map(({ timeSeconds, secondaryTexts }) => [timeSeconds, secondaryTexts])).toEqual([
      [1, ['和你一起走过']], [3, ['抬头望向天空']], [5, ['牵起了你的手']],
    ])
  })

  it('does not shift isolated or independently timed multilingual lines', () => {
    const isolated = parseLyrics('[00:01]君と歩いた\n[00:03]和你一起走过\n[00:03]空を見上げた')
    expect(isolated.lines[0].secondaryTexts).toBeUndefined()
    expect(isolated.lines[1].timeSeconds).toBe(3)
    const separate = parseLyrics('[00:01]君と歩いた\n[00:02]和你一起走过\n[00:03]空を見上げた\n[00:04]抬头望向天空\n[00:05]手をつないだ\n[00:06]牵起了你的手')
    expect(separate.lines.map((line) => line.timeSeconds)).toEqual([1, 2, 3, 4, 5, 6])
    expect(separate.lines.every((line) => !line.secondaryTexts)).toBe(true)
  })

  it('preserves unsynchronized bilingual text and ignores known metadata', () => {
    const parsed = parseLyrics('[ml:1.0]\n君と歩いた\n和你一起走过')
    expect(parsed.synced).toBe(false)
    expect(parsed.lines.map((line) => line.text)).toEqual(['君と歩いた', '和你一起走过'])
  })

  it('keeps every timestamp even when adjacent lines repeat a temporal pattern', () => {
    const content = '[00:01]First\n[00:03]Uno\n[00:03]Second\n[00:05]Dos\n[00:05]Third\n[00:07]Tres'
    const parsed = parseLyrics(content)
    expect(parsed.lines.map((line) => line.timeSeconds)).toEqual([1, 3, 5, 7])
    expect(parsed.lines[0].secondaryTexts).toBeUndefined()
    expect(parsed.lines[1]).toMatchObject({ text: 'Uno', secondaryTexts: ['Second'] })
    expect(parsed).not.toHaveProperty('suggestedLayout')
  })

  it('lets users choose the last line as primary without changing timing', () => {
    const parsed = parseLyrics('[00:01]译文\n[00:01]Original\n[00:03]Second', { primaryLine: 'last' })
    expect(parsed.lines[0]).toMatchObject({ timeSeconds: 1, text: 'Original', secondaryTexts: ['译文'] })
    expect(parsed.lines[1].timeSeconds).toBe(3)
  })

  it('preserves independent simultaneous lines when grouping is disabled', () => {
    const parsed = parseLyrics('[00:01]First\n[00:01]Second', { layout: 'separate' })
    expect(parsed.lines.map((line) => [line.timeSeconds, line.text, line.secondaryTexts])).toEqual([
      [1, 'First', undefined], [1, 'Second', undefined],
    ])
  })
})
