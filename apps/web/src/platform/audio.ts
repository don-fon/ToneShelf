export interface OriginalAudioSource {
  url: string
  contentType: string
}
export interface AudioEngine {
  load(url: string, original?: OriginalAudioSource): void
  play(): Promise<void>
  pause(): void
  seek(seconds: number): void
  setVolume(value: number): void
  position(): number
  duration(): number
  element(): HTMLAudioElement
}
export class WebAudioEngine implements AudioEngine {
  private readonly audio = new Audio()
  private sourceKey = ''
  private fallbackURL: string | null = null
  private pendingPosition: number | null = null
  private lastPosition = 0
  private wantsPlaying = false

  constructor() {
    this.audio.addEventListener('loadedmetadata', () => {
      if (this.pendingPosition !== null) {
        this.audio.currentTime = this.pendingPosition
        this.pendingPosition = null
      }
    })
    this.audio.addEventListener('timeupdate', () => {
      if (this.pendingPosition === null) this.lastPosition = this.audio.currentTime
    })
    this.audio.addEventListener('error', () => {
      if (this.useFallback() && this.wantsPlaying) void this.audio.play().catch(() => {})
    })
  }
  load(url: string, original?: OriginalAudioSource) {
    const preferred = original?.url && original.contentType && this.audio.canPlayType(original.contentType) ? original.url : url
    const key = preferred + '|' + url
    if (this.sourceKey === key) return
    this.sourceKey = key
    this.fallbackURL = preferred !== url ? url : null
    this.pendingPosition = null
    this.lastPosition = 0
    this.audio.src = preferred
  }
  async play() {
    this.wantsPlaying = true
    const source = this.audio.src
    try { await this.audio.play() } catch (reason) {
      if (reason instanceof DOMException && reason.name === 'NotSupportedError' && (source !== this.audio.src || this.useFallback())) {
        await this.audio.play()
      } else throw reason
    }
  }
  pause() { this.wantsPlaying = false; this.audio.pause() }
  seek(seconds: number) {
    this.lastPosition = seconds
    if (this.audio.readyState >= 1) {
      this.audio.currentTime = seconds
      this.pendingPosition = null
    } else this.pendingPosition = seconds
  }
  private useFallback() {
    if (!this.fallbackURL) return false
    const position = this.pendingPosition ?? (this.audio.currentTime || this.lastPosition)
    const url = this.fallbackURL
    this.fallbackURL = null
    this.pendingPosition = position
    this.audio.src = url
    return true
  }
  setVolume(value: number) { this.audio.volume = Math.max(0, Math.min(1, value)) }
  position() { return this.audio.currentTime || 0 }
  duration() { return this.audio.duration || 0 }
  element() { return this.audio }
}
