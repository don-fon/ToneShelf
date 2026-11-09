import { useMemo } from 'react'
import type { LyricsLayout, LyricsPrimaryLine } from '@music/domain'
import { parseLyrics } from '@music/player-policy'
import { LyricsPreview } from './LyricsPreview'

interface Props {
  lyrics: string
  layout?: LyricsLayout
  primaryLine?: LyricsPrimaryLine
  onChange: (options: { lyricsLayout: LyricsLayout; lyricsPrimaryLine: LyricsPrimaryLine }) => void
}

export function LyricsDisplaySettings({ lyrics, layout = 'timestamp', primaryLine = 'first', onChange }: Props) {
  const parsed = useMemo(() => parseLyrics(lyrics, { layout, primaryLine }), [lyrics, layout, primaryLine])
  return <div className="lyrics-display-settings">
    <div className="lyrics-display-row"><label>分组方式<select value={layout} onChange={(event) => onChange({ lyricsLayout: event.target.value as LyricsLayout, lyricsPrimaryLine: primaryLine })}>
      <option value="timestamp">按时间戳分组</option>
      <option value="separate">每行独立显示</option>
    </select></label>
    <label>主行<select value={primaryLine} disabled={layout === 'separate'} onChange={(event) => onChange({ lyricsLayout: layout, lyricsPrimaryLine: event.target.value as LyricsPrimaryLine })}>
      <option value="first">1</option>
      <option value="last">2</option>
    </select></label>
    </div>
    <LyricsPreview parsed={parsed}/>
  </div>
}
