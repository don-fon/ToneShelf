import { useEffect, useMemo, useRef } from 'react'
import { activeLyricIndex, parseLyrics } from '@music/player-policy'

interface LyricsViewProps {
  trackTitle?: string
  lyrics: string
  position: number
  onSeek: (seconds: number) => void
}

export function LyricsView({ trackTitle, lyrics, position, onSeek }: LyricsViewProps) {
  const parsed = useMemo(() => parseLyrics(lyrics), [lyrics])
  const activeIndex = parsed.synced ? activeLyricIndex(parsed.lines, position) : -1
  const activeRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    if (activeRef.current && typeof activeRef.current.scrollIntoView === 'function') {
      activeRef.current.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [activeIndex])

  if (!trackTitle) return null
  return <section className="lyrics-panel" aria-label="播放歌词">
    <header><div><small>正在播放</small><h2>{trackTitle}</h2></div><span>{parsed.synced ? 'LRC 同步歌词' : '歌词'}</span></header>
    {!parsed.lines.length ? <div className="lyrics-empty">暂无歌词，可在歌曲设置中添加或在线匹配。</div> :
      <div className={`lyrics-scroll ${parsed.synced ? 'synced' : 'plain'}`}>
        {parsed.lines.map((line, index) => parsed.synced && line.timeSeconds !== null ?
          <button type="button" key={line.id} ref={index === activeIndex ? activeRef : null} aria-current={index === activeIndex ? 'true' : undefined} className={index === activeIndex ? 'active' : ''} onClick={() => onSeek(line.timeSeconds!)}>{line.text}</button> :
          <p key={line.id}>{line.text}</p>)}
      </div>}
  </section>
}
