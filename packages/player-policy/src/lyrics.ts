export interface LyricLine {
  id: string
  timeSeconds: number | null
  text: string
}

export interface ParsedLyrics {
  synced: boolean
  lines: LyricLine[]
}

const metadataPattern = /^\[(?:ar|ti|al|by|re|ve|length):/i
const offsetPattern = /^\[offset:([+-]?\d+)\]$/i

export function parseLyrics(content: string): ParsedLyrics {
  const timed: Array<LyricLine & { order: number }> = []
  const plain: LyricLine[] = []
  const sourceLines = content.replace(/^\uFEFF/, '').split(/\r?\n/)
  const offsetMilliseconds = sourceLines.reduce((current, rawLine) => {
    const match = rawLine.trim().match(offsetPattern)
    return match ? Number(match[1]) : current
  }, 0)

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
      plain.push({ id: `plain-${order}`, timeSeconds: null, text: line })
      return
    }
    const text = line.replace(/\[(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?\]/g, '').trim()
    if (!text) return
    timestamps.forEach((match, index) => {
      const fraction = match[3] ? Number(`0.${match[3].padEnd(3, '0')}`) : 0
      const seconds = Number(match[1]) * 60 + Number(match[2]) + fraction + offsetMilliseconds / 1000
      timed.push({ id: `time-${order}-${index}`, timeSeconds: Math.max(0, seconds), text, order })
    })
  })

  if (!timed.length) return { synced: false, lines: plain }
  timed.sort((a, b) => (a.timeSeconds ?? 0) - (b.timeSeconds ?? 0) || a.order - b.order)
  return { synced: true, lines: timed.map(({ order: _order, ...line }) => line) }
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
