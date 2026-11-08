import { describe, expect, it } from 'vitest'
import { parseLyrics } from './lyrics'

describe('explicit kana annotations', () => {
  it('uses explicit title metadata for a zero-time header while originals stay on main row 1', () => {
    const body = '[00:00]広告告知\n[00:00]山の歌 - Name\n[00:01]川\n[00:01]河流'
    const source = '[ti:山の歌 (中文)]\n[kana:1やま1うた1かわ]\n' + body
    const parsed = parseLyrics(source)
    expect(parsed.lines[0].text).toBe('広告告知')
    expect(parsed.lines[0].ruby).toBeUndefined()
    expect(parsed.lines[0].secondaryRuby?.[0]?.filter(part => part.reading)).toEqual([
      { text: '山', reading: 'やま' }, { text: '歌', reading: 'うた' },
    ])
    expect(parsed.lines[1]).toMatchObject({ timeSeconds: 1, text: '川', ruby: [{ text: '川', reading: 'かわ' }] })
    expect(parsed.lines[1].secondaryRuby).toBeUndefined()
    const advanced = parseLyrics('[offset:500]\n' + source)
    expect(advanced.lines[0].timeSeconds).toBe(-.5)
    expect(advanced.lines[1].ruby).toEqual(parsed.lines[1].ruby)
    expect(parseLyrics('[kana:1やま1うた1かわ]\n' + body).lines.every(line => !line.ruby && !line.secondaryRuby)).toBe(true)
    const conflicting = '[ti:別の歌]\n' + source
    expect(parseLyrics(conflicting).lines.every(line => !line.ruby && !line.secondaryRuby)).toBe(true)
    const laterHeader = source.replaceAll('[00:00]', '[00:00.50]')
    expect(parseLyrics(laterHeader).lines.every(line => !line.ruby && !line.secondaryRuby)).toBe(true)
  })

  it('uses the chosen primary row only when both source candidates match completely', () => {
    const source = '[kana:1あ1い]\n[00:01]甲\n[00:01]丙\n[00:03]乙\n[00:03]丁'
    const first = parseLyrics(source)
    expect(first.lines[0].ruby).toEqual([{ text: '甲', reading: 'あ' }])
    expect(first.lines[0].secondaryRuby).toBeUndefined()
    const last = parseLyrics(source, { primaryLine: 'last' })
    expect(last.lines[0].ruby).toEqual([{ text: '丙', reading: 'あ' }])
    expect(last.lines[0].secondaryRuby).toBeUndefined()
    expect(parseLyrics(source, { layout: 'separate' }).lines.every((line) => !line.ruby)).toBe(true)
  })

  it('counts consecutive half-width or full-width digits as one explicit slot', () => {
    expect(parseLyrics('[kana:11ねこ]\n[00:01]９６猫').lines[0].ruby).toEqual([
      { text: '９６' }, { text: '猫', reading: 'ねこ' },
    ])
    expect(parseLyrics('[kana:1にじゅうに1さん]\n[00:01]A22B33').lines[0].ruby).toEqual([
      { text: 'A' }, { text: '22', reading: 'にじゅうに' }, { text: 'B' }, { text: '33', reading: 'さん' },
    ])
    expect(parseLyrics('[kana:1しめ]\n[00:01]〆').lines[0].ruby).toEqual([{ text: '〆', reading: 'しめ' }])
  })

  it('binds a uniquely matching source to its text regardless of primary-line styling', () => {
    const source = '[kana:1やま1かわ]\n[00:01]高山\n[00:01]山\n[00:03]河流\n[00:03]川\n[00:05]尾注'
    const first = parseLyrics(source)
    expect(first.lines[0].ruby).toBeUndefined()
    expect(first.lines[0].secondaryRuby).toEqual([[{ text: '山', reading: 'やま' }]])
    const last = parseLyrics(source, { primaryLine: 'last' })
    expect(last.lines[0]).toMatchObject({ timeSeconds: 1, text: '山', ruby: [{ text: '山', reading: 'やま' }] })
    expect(last.lines[1]).toMatchObject({ timeSeconds: 3, text: '川', ruby: [{ text: '川', reading: 'かわ' }] })
    expect(last.lines[2]).toMatchObject({ timeSeconds: 5, text: '尾注' })
    expect(last.lines[2].ruby).toBeUndefined()
    expect(last.lines[0].secondaryRuby).toBeUndefined()
    expect(parseLyrics(source, { layout: 'separate' }).lines.filter((line) => line.ruby).map((line) => line.text)).toEqual(['山', '川'])
    const corrected = '[kana:1やま1かわ]\n[00:01]山\n[00:01]高山\n[00:03]川\n[00:03]河流\n[00:05]尾注'
    expect(parseLyrics(corrected).lines[0].ruby).toEqual([{ text: '山', reading: 'やま' }])
  })

  it('includes complete trailing originals but never skips an internal unmatched row', () => {
    const source = '[kana:1やま1かわ1うみ]\n[00:01]高山\n[00:01]山\n[00:03]河流\n[00:03]川\n[00:05]海'
    expect(parseLyrics(source, { primaryLine: 'last' }).lines[2].ruby).toEqual([{ text: '海', reading: 'うみ' }])
    const corrupted = '[kana:1やま1かわ]\n[00:01]高山\n[00:01]山\n[00:02]插入文字\n[00:03]河流\n[00:03]川'
    expect(parseLyrics(corrupted, { primaryLine: 'last' }).lines.every((line) => !line.ruby)).toBe(true)
  })

  it('annotates timed text while keeping metadata, repeated timestamps and offset independent', () => {
    const source = '[ar:歌手]\n[kana:1やま1かわ]\n[offset:500]\n[00:01][00:03]山と川'
    const parsed = parseLyrics(source)
    expect(parsed.lines.map((line) => line.timeSeconds)).toEqual([.5, 2.5])
    for (const line of parsed.lines) {
      expect(line.text).toBe('山と川')
      expect(line.ruby).toEqual([{ text: '山', reading: 'やま' }, { text: 'と' }, { text: '川', reading: 'かわ' }])
    }
    expect(source).toContain('[kana:1やま1かわ]')
  })

  it('supports explicit placeholders, compound words and static display of timed readings', () => {
    expect(parseLyrics('[kana:111やま]\n[00:01]序文山').lines[0].ruby).toEqual([
      { text: '序文' }, { text: '山', reading: 'やま' },
    ])
    expect(parseLyrics('[kana:2きょう]\n[00:01]今日').lines[0].ruby).toEqual([{ text: '今日', reading: 'きょう' }])
    expect(parseLyrics('[kana:1や(100,200)ま(300,100)]\n[00:01]山').lines[0].ruby).toEqual([{ text: '山', reading: 'やま' }])
  })

  it('keeps annotations with the correct text when groups switch their primary line', () => {
    const source = '[kana:1やま]\n[00:01]山\n[00:01]Mountain'
    const first = parseLyrics(source).lines[0]
    expect(first.ruby).toEqual([{ text: '山', reading: 'やま' }])
    const last = parseLyrics(source, { primaryLine: 'last' }).lines[0]
    expect(last.text).toBe('Mountain')
    expect(last.ruby).toBeUndefined()
    expect(last.secondaryTexts).toEqual(['山'])
    expect(last.secondaryRuby).toEqual([[{ text: '山', reading: 'やま' }]])
    const inverse = parseLyrics('[kana:1やま]\n[00:01]Mountain\n[00:01]山', { primaryLine: 'last' }).lines[0]
    expect(inverse.text).toBe('山')
    expect(inverse.ruby).toEqual([{ text: '山', reading: 'やま' }])
    expect(parseLyrics(source, { layout: 'separate' }).lines[0].ruby).toEqual(first.ruby)
  })

  it('leaves text unchanged when counts, boundaries, tags or readings are ambiguous', () => {
    for (const source of [
      '[kana:1やま]\n[00:01]山川',
      '[kana:2きょう]\n[00:01]今は日',
      '[kana:2きょう]\n[00:01]今\n[00:02]日',
      '[kana:0やま]\n[00:01]山',
      '[kana:1山]\n[00:01]山',
      '[kana:1や(100,broken)ま]\n[00:01]山',
      '[kana:1やま]\n[kana:1かわ]\n[00:01]山',
    ]) {
      const parsed = parseLyrics(source)
      expect(parsed.lines.every((line) => !line.ruby)).toBe(true)
      expect(parsed.lines.every((line) => !line.text.includes('[kana:'))).toBe(true)
    }
  })

  it('handles plain lyrics and Unicode text without splitting surrogate pairs', () => {
    const plain = parseLyrics('[kana:1やま]\n🌙山')
    expect(plain.synced).toBe(false)
    expect(plain.lines[0].ruby).toEqual([{ text: '🌙' }, { text: '山', reading: 'やま' }])
    expect(parseLyrics('[kana:malformed]\nplain').lines.map((line) => line.text)).toEqual(['plain'])
  })
})
