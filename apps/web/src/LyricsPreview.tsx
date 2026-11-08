import type { ParsedLyrics } from '@music/player-policy'

const timestamp = (seconds: number) => {
  const milliseconds = Math.round(Math.abs(seconds) * 1000)
  return (seconds < 0 ? '-' : '') + Math.floor(milliseconds / 60000) + ':' + (milliseconds % 60000 / 1000).toFixed(3).padStart(6, '0')
}

export function LyricsPreview({ parsed, label = '歌词显示预览' }: { parsed: ParsedLyrics; label?: string }) {
  return <div className="lyrics-display-preview" role="region" aria-label={label}>
    {parsed.lines.length ? parsed.lines.map((line) => <p key={line.id}>
      {line.timeSeconds !== null && <small>{timestamp(line.timeSeconds)}</small>}
      <span>{line.text}</span>
      {line.secondaryTexts?.map((text, index) => <small key={index}>{text}</small>)}
    </p>) : <small>添加歌词后可预览。</small>}
  </div>
}
