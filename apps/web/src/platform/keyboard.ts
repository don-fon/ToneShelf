import { useEffect, useRef } from 'react'

interface Actions {
  enabled: boolean
  togglePlayback: () => void
  next: () => void
  seekBy: (seconds: number) => void
  adjustVolume: (delta: number) => void
  toggleMute: () => void
  showHelp: () => void
}

export const shortcutDescriptions = [
  ['空格', '播放 / 暂停'], ['N', '下一首'], ['← / →', '后退 / 前进 5 秒'],
  ['↓ / ↑', '主音量降低 / 提高 5%'], ['M', '静音 / 恢复音量'], ['?', '查看快捷键'],
] as const

export function usePlaybackShortcuts(actions: Actions) {
  const latest = useRef(actions)
  useEffect(() => { latest.current = actions }, [actions])
  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      if (!latest.current.enabled || event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.ctrlKey || event.metaKey || event.altKey || (event.shiftKey && event.key !== '?')) return
      const target = event.target instanceof Element ? event.target : document.activeElement
      if (target?.closest('input,textarea,select,[contenteditable]:not([contenteditable=false]),[role=textbox],[role=slider]') ||
        document.querySelector('.modal,dialog[open],[role=dialog][aria-modal=true]')) return
      if (event.key === ' ' && target?.closest('button,a,summary')) return
      const current = latest.current
      const handlers: Record<string, () => void> = {
        ' ': current.togglePlayback, n: current.next, m: current.toggleMute, '?': current.showHelp,
        arrowleft: () => current.seekBy(-5), arrowright: () => current.seekBy(5),
        arrowdown: () => current.adjustVolume(-0.05), arrowup: () => current.adjustVolume(0.05),
      }
      const action = handlers[event.key.toLowerCase()]
      if (!action) return
      event.preventDefault()
      if (!event.repeat || event.key.startsWith('Arrow')) action()
    }
    document.addEventListener('keydown', handle)
    return () => document.removeEventListener('keydown', handle)
  }, [])
}
