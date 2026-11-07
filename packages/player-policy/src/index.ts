import type { Track } from '@music/domain'

export * from './lyrics'

export type RecoveryState = Record<string, number>

export function effectiveWeight(track: Track, recovery: RecoveryState): number {
  const steps = recovery[track.id]
  if (steps === undefined) return track.baseRandomWeight
  return track.baseRandomWeight * Math.min(1, Math.max(0, steps) / 3)
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
  let weighted = candidates.map((track) => ({ track, weight: effectiveWeight(track, recovery) }))
  if (weighted.every(({ weight }) => weight <= 0 || !Number.isFinite(weight))) {
    weighted = candidates.map((track) => ({ track, weight: track.baseRandomWeight }))
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
  const next: RecoveryState = {}
  for (const [id, steps] of Object.entries(recovery)) next[id] = Math.min(3, steps + 1)
  next[nextID] = 0
  return next
}
