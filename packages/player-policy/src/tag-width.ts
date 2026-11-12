import { wide, emojiPresentation, emoji, pictographic, marks } from './tag-width-data'

function contains(ranges: readonly number[], point: number): boolean {
  let low = 0, high = ranges.length / 2
  while (low < high) {
    const middle = (low + high) >>> 1
    if (ranges[middle * 2 + 1] < point) low = middle + 1
    else high = middle
  }
  return low * 2 < ranges.length && ranges[low * 2] <= point
}

// Fixed label-width policy, not a measurement of the active font.
export function tagWidthUnits(name: string): number {
  let total = 0, current = 0
  let emojiBase = false, joinable = false, joining = false, regional = false
  for (const char of name) {
    const point = char.codePointAt(0)!
    if (point === 0x200d) { joining = joinable; continue }
    if ((point === 0xfe0f || point === 0x20e3) && emojiBase && current) { current = 2; continue }
    if (contains(marks, point) || (point >= 0x1f3fb && point <= 0x1f3ff) || (point >= 0xe0020 && point <= 0xe007f)) continue
    const width = contains(wide, point) || contains(emojiPresentation, point) ? 2 : 1
    const isRegional = point >= 0x1f1e6 && point <= 0x1f1ff
    if ((joining && contains(pictographic, point)) || (regional && isRegional)) {
      current = Math.max(current, width)
      regional = false
    } else {
      total += current; current = width; regional = isRegional
    }
    emojiBase = contains(emoji, point)
    joinable = contains(pictographic, point)
    joining = false
  }
  return total + current
}
