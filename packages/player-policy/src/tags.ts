import type { Tag, Track } from '@music/domain'
import { tagWidthUnits } from './tag-width'

export const UNTAGGED_ID = '_untagged'
export function tagGroupNameError(name: string): string | null {
  const length = [...name.trim()].length
  return length < 1 || length > 4 ? '标签组名称需为 1–4 字' : null
}
export function tagNameError(name: string): string | null {
  const trimmed = name.trim()
  if (trimmed === '无') return '无为系统标签'
  const units = tagWidthUnits(trimmed)
  return units < 1 || units > 4 ? '最多两个全角或四个半角字符' : null
}
export const allTagIDs = (tags: Tag[]) => [UNTAGGED_ID, ...tags.map((tag) => tag.id)]
export function filterTracks(tracks: Track[], enabled: string[]): Track[] {
  const selected = new Set(enabled)
  return tracks.filter((track) => track.tagIds?.length ? track.tagIds.some((id) => selected.has(id)) : selected.has(UNTAGGED_ID))
}
