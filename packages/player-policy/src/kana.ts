export interface RubyPart {
  text: string
  reading?: string
}

const isHan = (char: string) => /[\p{Unified_Ideograph}々〇〆]/u.test(char)

// Decode only explicit character counts. Any mismatch leaves all text unchanged.
export function alignKana(payload: string, lines: Array<{ order: number; text: string }>): Map<number, RubyPart[]> {
  const result = new Map<number, RubyPart[]>()
  const cleaned = payload.replace(/\(\d+,\d+\)/g, '')
  if (!/^[1-9]/.test(cleaned)) return result
  const units = [...cleaned.matchAll(/([1-9])([^0-9]*)/g)].map((match) => ({ count: Number(match[1]), reading: match[2].trim().normalize('NFC') }))
  if (units.map((unit) => String(unit.count) + unit.reading).join('') !== cleaned.replace(/\s/g, '').normalize('NFC') ||
    units.some((unit) => unit.reading && !/^[\p{Script_Extensions=Hiragana}\p{Script_Extensions=Katakana}]+$/u.test(unit.reading))) return result
  const characters = lines.map((line) => Array.from(line.text))
  const positions: Array<{ line: number; start: number; end: number }> = []
  characters.forEach((chars, line) => {
    for (let index = 0; index < chars.length; index++) {
      if (isHan(chars[index])) {
        positions.push({ line, start: index, end: index + 1 })
      } else if (/[0-9０-９]/u.test(chars[index])) {
        const start = index
        while (index + 1 < chars.length && /[0-9０-９]/u.test(chars[index + 1])) index++
        positions.push({ line, start, end: index + 1 })
      }
    }
  })
  if (positions.length !== units.reduce((sum, unit) => sum + unit.count, 0)) return result
  const spans = new Map<number, Array<{ start: number; end: number; reading: string }>>()
  let consumed = 0
  for (const unit of units) {
    const start = positions[consumed]
    if (unit.reading) {
      const end = positions[consumed + unit.count - 1]
      if (start.line !== end.line) return result
      for (let index = consumed + 1; index < consumed + unit.count; index++) {
        if (positions[index].start !== positions[index - 1].end) return result
      }
      const values = spans.get(start.line) ?? []
      values.push({ start: start.start, end: end.end, reading: unit.reading })
      spans.set(start.line, values)
    }
    consumed += unit.count
  }
  for (const [index, values] of spans) {
    const parts: RubyPart[] = []
    let cursor = 0
    for (const span of values) {
      if (span.start > cursor) parts.push({ text: characters[index].slice(cursor, span.start).join('') })
      parts.push({ text: characters[index].slice(span.start, span.end).join(''), reading: span.reading })
      cursor = span.end
    }
    if (cursor < characters[index].length) parts.push({ text: characters[index].slice(cursor).join('') })
    result.set(lines[index].order, parts)
  }
  return result
}
