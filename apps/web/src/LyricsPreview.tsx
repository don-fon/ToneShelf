import type { ParsedLyrics } from '@music/player-policy'
import { LyricText } from './LyricText'

const timestamp = (seconds: number) => {
  const milliseconds = Math.round(Math.abs(seconds) * 1000)
  return (seconds < 0 ? '-' : '') + Math.floor(milliseconds / 60000) + ':' + (milliseconds % 60000 / 1000).toFixed(3).padStart(6, '0')
}

export function LyricsPreview({ parsed, label = '歌词显示预览' }: { parsed: ParsedLyrics; label?: string }) {
  return <div className="lyrics-display-preview" role="region" aria-label={label}>
    {parsed.lines.length ? parsed.lines.map((line) => <p key={line.id}>
      {line.timeSeconds !== null && <small>{timestamp(line.timeSeconds)}</small>}
      <span><LyricText text={line.text} ruby={line.ruby}/></span>
      {line.secondaryTexts?.map((text, index) => <small key={index}><LyricText text={text} ruby={line.secondaryRuby?.[index]}/></small>)}
    </p>) : <small>添加歌词后可预览。</small>}
  </div>
}
