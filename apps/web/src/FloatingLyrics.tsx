import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { Track } from '@music/domain'
import { activeLyricIndex, parseLyrics } from '@music/player-policy'
import { LyricText } from './LyricText'
import { TrackCover } from './TrackCover'
import { LYRICS_ANIMATION_LEAD_SECONDS } from './lyrics-timing'
import { openLyricsWindow } from './platform/picture-in-picture'

interface Props {
  track: Track | null
  position: number
  playing: boolean
  onToggle: () => void
  onNext: () => void
}

const styles = `*{box-sizing:border-box}html,body,#floating-lyrics-root{height:100%;margin:0}body{background:#211d1a;color:#fff5e9;font-family:Inter,"Segoe UI","Noto Sans SC",sans-serif}.pip-player{height:100%;position:relative;isolation:isolate;overflow:hidden;display:flex;flex-direction:column;padding:12px 16px;gap:10px}.pip-cover{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;opacity:.45;filter:blur(12px);transform:scale(1.12);pointer-events:none;z-index:-2}.pip-veil{position:absolute;inset:0;background:linear-gradient(180deg,#201b1860,#201b18b0);pointer-events:none;z-index:-1}.pip-lyrics{position:relative;flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:safe center;text-align:center;overflow-wrap:anywhere;line-height:1.7;gap:10px}.pip-lyrics p{margin:0;font-size:clamp(18px,5vw,26px);font-weight:600}.pip-lyrics .next{opacity:.55}.pip-lyrics .pip-exiting{position:absolute;left:0;width:100%;pointer-events:none}.pip-lyrics .translation{display:block;font-size:.65em;font-weight:400;color:#e0d4c7}.pip-lyrics rt{font-size:.45em;color:#ddba92}.pip-empty{color:#b0a397;font-size:14px}.pip-controls{position:absolute;bottom:12px;left:50%;transform:translateX(-50%);display:flex;align-items:center;gap:12px;opacity:0;pointer-events:none;transition:opacity .18s ease}.pip-player:hover .pip-controls,.pip-player.keyboard-controls:has(:focus-visible) .pip-controls{opacity:1;pointer-events:auto}.pip-controls button{display:grid;place-items:center;width:36px;height:36px;padding:0;border:1px solid #ffffff20;border-radius:50%;background:#201b1866;backdrop-filter:blur(12px);color:#fff5e9;cursor:pointer;transition:background .18s ease}.pip-controls button:hover{background:#ffffff25}.pip-controls button.main{width:40px;height:40px}.pip-controls button:focus-visible{outline:2px solid #d86e43;outline-offset:3px}.pip-controls svg{width:18px;height:18px;fill:currentColor;pointer-events:none}@media(hover:none){.pip-controls{opacity:1;pointer-events:auto}}@media(prefers-reduced-motion:reduce){.pip-controls,.pip-controls button{transition:none}}`

function Contents({ track, position, playing, onToggle, onNext }: Props) {
  const [keyboardControls, setKeyboardControls] = useState(true)
  const parsed = useMemo(() => parseLyrics(track?.lyrics ?? '', { layout: track?.lyricsLayout, primaryLine: track?.lyricsPrimaryLine }), [track?.lyrics, track?.lyricsLayout, track?.lyricsPrimaryLine])
  const index = parsed.synced ? activeLyricIndex(parsed.lines, position + LYRICS_ANIMATION_LEAD_SECONDS) : -1
  const currentTime = index < 0 ? null : parsed.lines[index].timeSeconds
  const nextTime = parsed.synced ? parsed.lines.find((line) => line.timeSeconds !== null && (currentTime === null || line.timeSeconds > currentTime))?.timeSeconds : undefined
  const lines = parsed.synced ? parsed.lines.filter((line) => line.timeSeconds === currentTime || line.timeSeconds === nextTime) : parsed.lines
  const lyricsRef = useRef<HTMLElement | null>(null)
  const previous = useRef<{ song: string | undefined; parsed: typeof parsed; nextTime: number | null | undefined; width: number; height: number; rows: Map<string, { top: number; height: number; opacity: string; copy: HTMLElement }> } | null>(null)
  useLayoutEffect(() => {
    const container = lyricsRef.current
    if (!container) return
    const bounds = container.getBoundingClientRect()
    const rows = new Map(Array.from(container.querySelectorAll<HTMLElement>('p[data-line-id]'), (row) => {
      const rect = row.getBoundingClientRect()
      return [row.dataset.lineId!, { top: rect.top - bounds.top + container.scrollTop, height: rect.height, opacity: row.classList.contains('next') ? '.55' : '1', copy: row.cloneNode(true) as HTMLElement }] as const
    }))
    const old = previous.current
    previous.current = { song: track?.id, parsed, nextTime, width: bounds.width, height: bounds.height, rows }
    if (!parsed.synced || !old || old.song !== track?.id || old.parsed !== parsed || old.nextTime !== currentTime || old.width !== bounds.width || old.height !== bounds.height || !container.animate || container.ownerDocument.defaultView?.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const animations: Animation[] = []
    const outgoing: HTMLElement[] = []
    const options = { duration: 300, easing: 'cubic-bezier(.22,.61,.36,1)' }
    let distance = 0
    for (const [id, row] of rows) {
      const before = old.rows.get(id)
      if (before) { distance = before.top - row.top; break }
    }
    for (const row of container.querySelectorAll<HTMLElement>('p[data-line-id]')) {
      const current = rows.get(row.dataset.lineId!)!
      const before = old.rows.get(row.dataset.lineId!)
      animations.push(row.animate([
        { transform: `translateY(${before ? before.top - current.top : Math.max(18, distance || current.height + 10)}px)`, opacity: before?.opacity ?? '0' },
        { transform: 'translateY(0)', opacity: current.opacity },
      ], options))
    }
    for (const [id, row] of old.rows) {
      if (rows.has(id)) continue
      const copy = row.copy
      copy.removeAttribute('data-line-id'); copy.removeAttribute('aria-current'); copy.setAttribute('aria-hidden', 'true')
      copy.classList.add('pip-exiting')
      copy.style.top = `${row.top}px`
      container.append(copy); outgoing.push(copy)
      const animation = copy.animate([{ transform: 'translateY(0)', opacity: row.opacity }, { transform: `translateY(${-Math.max(18, distance || row.height + 10)}px)`, opacity: '0' }], options)
      animation.onfinish = () => copy.remove()
      animations.push(animation)
    }
    return () => { animations.forEach((animation) => animation.cancel()); outgoing.forEach((row) => row.remove()) }
  }, [parsed, currentTime, nextTime, track?.id])
  return <main className={`pip-player${keyboardControls ? ' keyboard-controls' : ''}`} onFocusCapture={(event) => { if (event.target.matches(':focus-visible')) setKeyboardControls(true) }} onKeyDownCapture={() => setKeyboardControls(true)} onPointerMove={() => setKeyboardControls(false)} onPointerDown={() => setKeyboardControls(false)} onPointerLeave={() => setKeyboardControls(false)}>
    <TrackCover src={track?.coverUrl} className="pip-cover" background/>
    <div className="pip-veil" aria-hidden="true"/>
    <section ref={lyricsRef} className="pip-lyrics" aria-label="悬浮歌词内容">
      {parsed.synced && index < 0 && parsed.lines.length > 0 && <span className="pip-empty">等待歌词…</span>}
      {lines.length ? lines.map((line) => <p key={line.id} data-line-id={line.id} className={parsed.synced && line.timeSeconds === nextTime ? 'next' : 'current'} aria-current={parsed.synced && line.timeSeconds === currentTime ? 'true' : undefined}><LyricText text={line.text} ruby={line.ruby}/>{line.secondaryTexts?.map((text, i) => <span className="translation" key={i}><LyricText text={text} ruby={line.secondaryRuby?.[i]}/></span>)}</p>) : !parsed.lines.length && <span className="pip-empty">暂无歌词</span>}
    </section>
    <footer className="pip-controls"><button className="main" type="button" aria-label={playing ? '暂停' : '播放'} title={playing ? '暂停' : '播放'} onClick={onToggle}><svg aria-hidden="true" viewBox="0 0 24 24">{playing ? <path d="M7 5h4v14H7zm6 0h4v14h-4z"/> : <path d="m9 5 11 7-11 7z"/>}</svg></button><button type="button" aria-label="下一首" title="下一首" onClick={onNext}><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m5 5 10 7-10 7zm11 0h3v14h-3z"/></svg></button></footer>
  </main>
}

export function FloatingLyrics(props: Props) {
  const [target, setTarget] = useState<HTMLElement | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const pip = useRef<Window | null>(null)
  const mounted = useRef(false)
  const opening = useRef(false)
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false; pip.current?.close(); pip.current = null }
  }, [])
  useEffect(() => { if (!props.track && target) pip.current?.close() }, [props.track?.id, target])
  const toggle = async () => {
    if (pip.current && !pip.current.closed) { pip.current.close(); return }
    if (opening.current) return
    opening.current = true; setPending(true); setError('')
    try {
      const window = await openLyricsWindow()
      if (!mounted.current) { window.close(); return }
      pip.current = window
      window.document.title = '悬浮歌词'
      const style = window.document.createElement('style'); style.textContent = styles; window.document.head.append(style)
      const root = window.document.createElement('div'); root.id = 'floating-lyrics-root'; window.document.body.append(root)
      window.addEventListener('pagehide', () => {
        if (pip.current !== window) return
        pip.current = null
        if (mounted.current) setTarget(null)
      }, { once: true })
      setTarget(root)
    } catch (reason) {
      pip.current?.close(); pip.current = null
      if (mounted.current) { setTarget(null); setError(reason instanceof Error ? reason.message : '无法打开悬浮歌词，请重试。') }
    } finally {
      opening.current = false
      if (mounted.current) setPending(false)
    }
  }
  return <>
    <button className="floating-lyrics-toggle" type="button" aria-label={target ? '关闭悬浮歌词' : '悬浮歌词'} aria-pressed={Boolean(target)} title={props.track ? target ? '关闭悬浮歌词' : '悬浮歌词（画中画）' : '请先选择一首歌曲'} disabled={!props.track || pending} onClick={() => void toggle()}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M12 11h6v5h-6z"/></svg></button>
    {target && createPortal(<Contents {...props}/>, target)}
    {error && createPortal(<div className="floating-lyrics-notice" role="alert">{error}<button type="button" aria-label="关闭悬浮歌词提示" onClick={() => setError('')}>×</button></div>, document.body)}
  </>
}
