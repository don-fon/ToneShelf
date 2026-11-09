import { useRef, useState, type ReactNode } from 'react'
import type { LyricsLayout, LyricsPrimaryLine } from '@music/domain'
import { LyricsDisplaySettings } from './LyricsDisplaySettings'

interface Props {
  lyrics: string
  layout?: LyricsLayout
  primaryLine?: LyricsPrimaryLine
  onTextChange: (text: string) => void
  onDisplayChange: (options: { lyricsLayout: LyricsLayout; lyricsPrimaryLine: LyricsPrimaryLine }) => void
  actions?: ReactNode
  searchResults?: ReactNode
}

export function LyricsEditor({ lyrics, layout, primaryLine, onTextChange, onDisplayChange, actions, searchResults }: Props) {
  const [tab, setTab] = useState<'edit' | 'display'>('edit')
  const tabs = useRef<HTMLDivElement>(null)
  return <section className="lyrics-editor" aria-label="歌词编辑与显示">
    <div className="lyrics-tools"><div className="lyrics-heading"><span>歌词</span><div className="lyrics-tabs" role="tablist" aria-label="歌词编辑模式" ref={tabs} onKeyDown={(event) => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
      event.preventDefault()
      const next = event.key === 'Home' ? 'edit' : event.key === 'End' ? 'display' : tab === 'edit' ? 'display' : 'edit'
      setTab(next); tabs.current?.querySelector<HTMLButtonElement>(`[data-tab="${next}"]`)?.focus()
    }}>
      <button type="button" id="lyrics-edit-tab" role="tab" data-tab="edit" aria-selected={tab === 'edit'} aria-controls="lyrics-edit-panel" tabIndex={tab === 'edit' ? 0 : -1} onClick={() => setTab('edit')}>编辑</button>
      <button type="button" id="lyrics-display-tab" role="tab" data-tab="display" aria-selected={tab === 'display'} aria-controls="lyrics-display-panel" tabIndex={tab === 'display' ? 0 : -1} onClick={() => setTab('display')}>显示设置</button>
    </div></div>{actions}</div>
    {searchResults}
    <div className="lyrics-workspace">
      <div id="lyrics-edit-panel" role="tabpanel" aria-labelledby="lyrics-edit-tab" hidden={tab !== 'edit'}><textarea aria-label="歌词" value={lyrics} onChange={(event) => onTextChange(event.target.value)} spellCheck={false}/></div>
      <div id="lyrics-display-panel" role="tabpanel" aria-labelledby="lyrics-display-tab" hidden={tab !== 'display'}><LyricsDisplaySettings lyrics={lyrics} layout={layout} primaryLine={primaryLine} onChange={onDisplayChange}/></div>
    </div>
  </section>
}
