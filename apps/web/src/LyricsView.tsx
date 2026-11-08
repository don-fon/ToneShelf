import { useEffect, useMemo, useRef } from 'react'
import type { LyricsLayout, LyricsPrimaryLine } from '@music/domain'
import { LyricText } from './LyricText'
import { activeLyricIndex, parseLyrics } from '@music/player-policy'

interface LyricsViewProps {
  trackTitle?: string
  lyrics: string
  layout?: LyricsLayout
  primaryLine?: LyricsPrimaryLine
  position: number
  onSeek: (seconds: number) => void
}

export function LyricsView({ trackTitle, lyrics, layout, primaryLine, position, onSeek }: LyricsViewProps) {
  const parsed = useMemo(() => parseLyrics(lyrics, { layout, primaryLine }), [lyrics, layout, primaryLine])
  const activeIndex = parsed.synced ? activeLyricIndex(parsed.lines, position) : -1
  const activeRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    if (activeRef.current && typeof activeRef.current.scrollIntoView === 'function') {
      activeRef.current.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
  }, [activeIndex, lyrics, layout, primaryLine])

  if (!trackTitle) return null
  return <section className="lyrics-panel" aria-label="播放歌词">
    <header><div><small>正在播放</small><h2>{trackTitle}</h2></div><span>{parsed.synced ? 'LRC 同步歌词' : '歌词'}</span></header>
    {!parsed.lines.length ? <div className="lyrics-empty">暂无歌词，可在歌曲设置中添加或在线匹配。</div> :
      <div className={`lyrics-scroll ${parsed.synced ? 'synced' : 'plain'}`}>
        {parsed.lines.map((line, index) => parsed.synced && line.timeSeconds !== null ?
          <button type="button" key={line.id} ref={index === activeIndex ? activeRef : null}
            aria-label={[line.text, ...(line.secondaryTexts ?? [])].join(' ')}
            aria-current={activeIndex >= 0 && line.timeSeconds === parsed.lines[activeIndex].timeSeconds ? 'true' : undefined}
            className={activeIndex >= 0 && line.timeSeconds === parsed.lines[activeIndex].timeSeconds ? 'active' : ''} onClick={() => onSeek(Math.max(0, line.timeSeconds!))}>
            <span className="lyric-original"><LyricText text={line.text} ruby={line.ruby}/></span>
            {line.secondaryTexts?.map((text, secondaryIndex) => <span className="lyric-secondary" key={secondaryIndex}><LyricText text={text} ruby={line.secondaryRuby?.[secondaryIndex]}/></span>)}
          </button> :
          <p key={line.id}><LyricText text={line.text} ruby={line.ruby}/></p>)}
      </div>}
  </section>
}
