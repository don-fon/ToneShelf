import { useEffect, useRef } from 'react'
import { shortcutDescriptions } from './platform/keyboard'

export function ShortcutHelp({ onClose }: { onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = ref.current!
    if (dialog.showModal) dialog.showModal()
    else dialog.setAttribute('open', '')
    return () => { if (dialog.open && dialog.close) dialog.close() }
  }, [])
  return <dialog ref={ref} className="shortcut-help" aria-modal="true" aria-labelledby="shortcut-help-title" onClose={(event) => { if (!event.currentTarget.open) onClose() }}>
    <header><h2 id="shortcut-help-title">快捷键</h2><button type="button" aria-label="关闭快捷键说明" onClick={onClose} autoFocus>×</button></header>
    <dl>{shortcutDescriptions.map(([key, description]) => <div key={key}><dt><kbd>{key}</kbd></dt><dd>{description}</dd></div>)}</dl>
    <p>输入文字、调整滑块或打开弹窗时，不触发播放快捷键。按 Esc 关闭说明。</p>
  </dialog>
}
