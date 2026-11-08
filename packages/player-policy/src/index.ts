import type { Track } from '@music/domain'

export * from './lyrics'

export type RecoveryState = Record<string, number>

export function effectiveWeight(track: Track, recovery: RecoveryState, playlistSize: number): number {
  const steps = recovery[track.id]
  if (steps === undefined) return track.baseRandomWeight
  return track.baseRandomWeight * Math.min(1, Math.max(0, steps) / Math.max(1, playlistSize / 2))
}
export function selectNext(
  tracks: Track[],
  currentID: string | null,
  recovery: RecoveryState,
  random: () => number = Math.random,
): Track | null {
  if (tracks.length === 0) return null
  if (tracks.length === 1) return tracks[0]
  const candidates = tracks.filter((track) => track.id !== currentID)
  const rested = candidates.filter((track) => recovery[track.id] === undefined || recovery[track.id] >= 2)
  const pool = rested.length ? rested : candidates
  let weighted = pool.map((track) => ({ track, weight: effectiveWeight(track, recovery, tracks.length) }))
  if (weighted.every(({ weight }) => weight <= 0 || !Number.isFinite(weight))) {
    weighted = pool.map((track) => ({ track, weight: track.baseRandomWeight }))
  }
  const total = weighted.reduce((sum, item) => sum + item.weight, 0)
  let cursor = Math.min(0.999999999999, Math.max(0, random())) * total
  for (const item of weighted) {
    cursor -= item.weight
    if (cursor < 0) return item.track
  }
  return weighted.at(-1)?.track ?? null
}
export function advanceRecovery(recovery: RecoveryState, nextID: string): RecoveryState {
  if (recovery[nextID] === 0) return { ...recovery }
  const next: RecoveryState = {}
  for (const [id, steps] of Object.entries(recovery)) next[id] = Math.min(Number.MAX_SAFE_INTEGER, steps + 1)
  next[nextID] = 0
  return next
}
export function sortTracks(tracks: Track[], field: 'uploadedAt' | 'fileName', direction: 'asc' | 'desc'): Track[] {
  const factor = direction === 'asc' ? 1 : -1
  const collator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' })
  return [...tracks].sort((a, b) => {
    const compared = field === 'uploadedAt'
      ? Date.parse(a.uploadedAt) - Date.parse(b.uploadedAt)
      : collator.compare(a.fileName, b.fileName)
    return factor * (compared || a.id.localeCompare(b.id))
  })
}
