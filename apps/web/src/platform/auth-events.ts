const eventName = 'music-auth-expired'
let generation = 0
export const authGeneration = () => generation
export const advanceAuthGeneration = () => { generation++ }
export const notifyAuthExpired = (expected = generation) => { if (expected === generation) window.dispatchEvent(new Event(eventName)) }
export function watchAuthExpired(callback: () => void) {
  window.addEventListener(eventName, callback)
  return () => window.removeEventListener(eventName, callback)
}
