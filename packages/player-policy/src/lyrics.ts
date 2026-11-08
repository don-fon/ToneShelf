import type { LyricsLayout, LyricsPrimaryLine } from '@music/domain'
import { alignKana } from './kana'
import type { RubyPart } from './kana'
export type { RubyPart } from './kana'

export interface LyricLine {
  id: string
  timeSeconds: number | null
  text: string
  secondaryTexts?: string[]
  ruby?: RubyPart[]
  secondaryRuby?: Array<RubyPart[] | undefined>
}

export interface ParsedLyrics {
  synced: boolean
  lines: LyricLine[]
}

const metadataPattern = /^\[(?:ar|ti|al|by|re|ve|length|ml|kana):/i
const offsetPattern = /^\[offset:([+-]?\d+)\]$/i

// Positive LRC offsets advance lyrics; negative values delay them.
export function lyricsOffsetSeconds(content: string): number {
  let milliseconds = 0
  for (const raw of content.split(/\r\n|\n|\r/)) {
    const match = raw.trim().match(offsetPattern)
    if (match && Number.isSafeInteger(Number(match[1]))) milliseconds = Number(match[1])
  }
  return milliseconds === 0 ? 0 : -milliseconds / 1000
}

export function parseLyrics(content: string, options: { layout?: LyricsLayout; primaryLine?: LyricsPrimaryLine } = {}): ParsedLyrics {
  const timed: Array<LyricLine & { order: number }> = []
  const plain: Array<LyricLine & { order: number }> = []
  const sourceLines = content.replace(/^\uFEFF/, '').split(/\r\n|\n|\r/)
  const offsetSeconds = lyricsOffsetSeconds(content)

  sourceLines.forEach((rawLine, order) => {
    const line = rawLine.trim()
    if (!line) return
    const offset = line.match(offsetPattern)
    if (offset) {
      return
    }
    if (metadataPattern.test(line)) return

    const timestamps = [...line.matchAll(/\[(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?\]/g)]
    if (!timestamps.length) {
      plain.push({ id: `plain-${order}`, timeSeconds: null, text: line, order })
      return
    }
    const text = line.replace(/\[(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?\]/g, '').trim()
    if (!text) return
    timestamps.forEach((match, index) => {
      const fraction = match[3] ? Number(`0.${match[3].padEnd(3, '0')}`) : 0
      const seconds = Number(match[1]) * 60 + Number(match[2]) + fraction + offsetSeconds
      timed.push({ id: `time-${order}-${index}`, timeSeconds: seconds, text, order })
    })
  })

  const kanaTags = sourceLines.flatMap((raw) => {
    const match = raw.trim().match(/^\[kana:(.*)\]$/i)
    return match ? [match[1]] : []
  })
  const lines = timed.length ? timed : plain
  if (new Set(kanaTags).size === 1) {
    const sources = [...new Map(lines.map((line) => [line.order, { order: line.order, text: line.text }])).values()]
    let ruby = alignKana(kanaTags[0], sources)
    if (!ruby.size && timed.length) {
      const groups = new Map<number, typeof timed>()
      for (const line of timed) {
        const values = groups.get(line.timeSeconds!) ?? []
        values.push(line)
        groups.set(line.timeSeconds!, values)
      }
      const values = [...groups.values()]
      const titleTags = sourceLines.flatMap((raw) => {
        const match = raw.trim().match(/^\[ti:(.*)\]$/i)
        return match?.[1].trim() ? [match[1].trim()] : []
      })
      const title = new Set(titleTags).size === 1 ? titleTags[0] : undefined
      const titleBase = title?.split(/\s*[（(]/, 1)[0].trim()
      const titleRow = (group: typeof timed) => {
        if (!titleBase || group[0].timeSeconds !== offsetSeconds) return undefined
        const matches = group.filter((line) => line.text === titleBase ||
          line.text.startsWith(titleBase + ' ') || line.text.startsWith(titleBase + '（') || line.text.startsWith(titleBase + '('))
        return matches.length === 1 ? matches[0] : undefined
      }
      const lastShared = values.reduce((last, group, index) => new Set(group.map((line) => line.text)).size > 1 ? index : last, -1)
      if (lastShared >= 0) {
        const matchPrimary = (primary: 'first' | 'last') => {
          const sourcesFor = (rows: typeof values) => [...new Map(rows.map((group) => {
            const line = titleRow(group) ?? (primary === 'last' ? group[group.length - 1] : group[0])
            return [line.order, { order: line.order, text: line.text }] as const
          })).values()].sort((a, b) => a.order - b.order)
          // No internal row or token is skipped. Only an uncovered trailing
          // single-line section may remain plain after the last shared group.
          const complete = alignKana(kanaTags[0], sourcesFor(values))
          return complete.size || lastShared === values.length - 1 ? complete :
            alignKana(kanaTags[0], sourcesFor(values.slice(0, lastShared + 1)))
        }
        const first = matchPrimary('first')
        const last = matchPrimary('last')
        if (first.size && !last.size) ruby = first
        else if (last.size && !first.size) ruby = last
        else if (options.layout !== 'separate') ruby = options.primaryLine === 'last' ? last : first
      }
    }
    for (const line of lines) {
      const parts = ruby.get(line.order)
      if (parts) line.ruby = parts
    }
  }
  if (!timed.length) return { synced: false, lines: plain.map(({ order: _order, ...line }) => line) }
  timed.sort((a, b) => (a.timeSeconds ?? 0) - (b.timeSeconds ?? 0) || a.order - b.order)
  if (options.layout === 'separate') {
    return { synced: true, lines: timed.map(({ order: _order, ...line }) => line) }
  }
  const grouped: LyricLine[] = []
  for (const { order: _order, ...line } of timed) {
    const previous = grouped.at(-1)
    if (previous && previous.timeSeconds === line.timeSeconds) {
      if (line.text !== previous.text && !previous.secondaryTexts?.includes(line.text)) {
        const count = previous.secondaryTexts?.length ?? 0
        previous.secondaryTexts = [...(previous.secondaryTexts ?? []), line.text]
        if (line.ruby || previous.secondaryRuby) previous.secondaryRuby = [...(previous.secondaryRuby ?? Array<RubyPart[] | undefined>(count).fill(undefined)), line.ruby]
      }
    } else {
      grouped.push(line)
    }
  }
  if (options.primaryLine === 'last') {
    for (const line of grouped) {
      if (line.secondaryTexts?.length) {
        const texts = [line.text, ...line.secondaryTexts]
        const readings = [line.ruby, ...(line.secondaryRuby ?? Array<RubyPart[] | undefined>(line.secondaryTexts.length).fill(undefined))]
        line.text = texts.pop()!
        line.secondaryTexts = texts
        line.ruby = readings.pop()
        line.secondaryRuby = readings.some(Boolean) ? readings : undefined
      }
    }
  }
  return { synced: true, lines: grouped }
}

export function activeLyricIndex(lines: LyricLine[], positionSeconds: number): number {
  let low = 0
  let high = lines.length - 1
  let active = -1
  while (low <= high) {
    const middle = Math.floor((low + high) / 2)
    const time = lines[middle]?.timeSeconds
    if (time !== null && time !== undefined && time <= positionSeconds) {
      active = middle
      low = middle + 1
    } else {
      high = middle - 1
    }
  }
  return active
}
