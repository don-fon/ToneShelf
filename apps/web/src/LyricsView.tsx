import { useEffect, useMemo, useRef } from 'react'
import type { LyricsLayout, LyricsPrimaryLine } from '@music/domain'
import { LyricText } from './LyricText'
import { activeLyricIndex, parseLyrics } from '@music/player-policy'

const LYRICS_ANIMATION_LEAD_SECONDS = 0.3

interface LyricsViewProps {
  trackTitle?: string
  lyrics: string
  layout?: LyricsLayout
  primaryLine?: LyricsPrimaryLine
  position: number
  viewKey?: string
  onSeek: (seconds: number) => void
}

export function LyricsView({ trackTitle, lyrics, layout, primaryLine, position, viewKey, onSeek }: LyricsViewProps) {
  const parsed = useMemo(() => parseLyrics(lyrics, { layout, primaryLine }), [lyrics, layout, primaryLine])
  const activeIndex = parsed.synced ? activeLyricIndex(parsed.lines, position + LYRICS_ANIMATION_LEAD_SECONDS) : -1
  const activeRef = useRef<HTMLButtonElement | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const container = scrollRef.current
    const line = activeRef.current
    if (!container || !line || typeof container.scrollTo !== 'function') return
    const center = () => {
      if (!container.clientHeight) return
      container.style.setProperty('--lyrics-edge', `${container.clientHeight / 2}px`)
      const row = line.getBoundingClientRect()
      const panel = container.getBoundingClientRect()
      container.scrollTo({ top: Math.max(0, container.scrollTop + row.top - panel.top - container.clientTop - (container.clientHeight - row.height) / 2), behavior: 'smooth' })
    }
    center()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(center)
    observer?.observe(container)
    return () => observer?.disconnect()
  }, [activeIndex, lyrics, layout, primaryLine, viewKey])

  if (!trackTitle) return null
  return <section className="lyrics-panel" aria-label="播放歌词">
    <header><div><small>正在播放</small><h2>{trackTitle}</h2></div><span>{parsed.synced ? 'LRC 同步歌词' : '歌词'}</span></header>
    {!parsed.lines.length ? <div className="lyrics-empty">暂无歌词，可在歌曲设置中添加或在线匹配。</div> :
      <div ref={scrollRef} className={`lyrics-scroll ${parsed.synced ? 'synced' : 'plain'}`} tabIndex={0} aria-label="歌词内容">
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
