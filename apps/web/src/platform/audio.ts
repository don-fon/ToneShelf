export interface AudioEngine {
  load(url: string): void
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
  load(url: string) { if (this.audio.src !== new URL(url, location.href).href) this.audio.src = url }
  play() { return this.audio.play() }
  pause() { this.audio.pause() }
  seek(seconds: number) { this.audio.currentTime = seconds }
  setVolume(value: number) { this.audio.volume = Math.max(0, Math.min(1, value)) }
  position() { return this.audio.currentTime || 0 }
  duration() { return this.audio.duration || 0 }
  element() { return this.audio }
}
