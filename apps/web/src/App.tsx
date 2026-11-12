import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Track } from '@music/domain'
import { advanceRecovery, selectNext, sortTracks, allTagIDs, filterTracks } from '@music/player-policy'
import { api } from './api'
import { createPortal } from 'react-dom'
import { useNarrowLayout } from './platform/layout'
import { TagPanel, TrackTagBadges, BulkTagDialog } from './Tags'
import './tags.css'
import { UploadTasks, audioAccept } from './UploadTasks'
import { CoverPicker } from './CoverPicker'
import { useSongSettingsSave } from './useSongSettingsSave'
import { TrackCover } from './TrackCover'
import { LyricsView } from './LyricsView'
import { FloatingLyrics } from './FloatingLyrics'
import { LyricsEditor } from './LyricsEditor'
import { WebAudioEngine } from './platform/audio'
import { usePlaybackShortcuts } from './platform/keyboard'
import { useMediaSession } from './platform/media-session'
import { ShortcutHelp } from './ShortcutHelp'
import { watchSessionLifecycle } from './platform/session'
import { flushPlayerSession, usePlayer } from './player-store'

const bytes = (value = 0) => value >= 1024 ** 3 ? `${Number((value / 1024 ** 3).toFixed(1))} GiB` : `${(value / 1024 ** 2).toFixed(1)} MiB`
const time = (value = 0) => `${Math.floor(value / 60)}:${Math.floor(value % 60).toString().padStart(2, '0')}`

const trackSubtitle = (track: Track) => {
  const album = track.album?.trim() ?? ''
  const normalized = (name: string) => name.trim().normalize('NFKC').toLowerCase()
  return (track.artist || '未知艺术家') + (album && normalized(album) !== normalized(track.title) ? ' · ' + album : '')
}

export function App({ onLogout, logoutBusy = false }: { onLogout?: () => void; logoutBusy?: boolean } = {}) {
  const query = useQueryClient()
  const { data: library, isLoading, error, dataUpdatedAt } = useQuery({ queryKey: ['library'], queryFn: api.library })
  const tracks = library?.tracks ?? []
  const tags = library?.tags ?? []
  const groups = library?.groups ?? []
  const { data: quota } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: trash = [] } = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const player = usePlayer()
  const compact = useNarrowLayout()
  const [selectionAnchor, setSelectionAnchor] = useState<HTMLDivElement | null>(null)
  const enabledTags = player.tagSelection ?? allTagIDs(tags)
  const filteredTracks = useMemo(() => filterTracks(tracks, player.tagSelection ?? allTagIDs(tags)), [library, player.tagSelection])
  const active = { id: player.playlistID ?? 'library', tracks: filteredTracks }
  const current = tracks.find((track) => track.id === player.trackID) ?? null
  const [engine] = useState(() => new WebAudioEngine())
  useEffect(() => () => engine.unload(), [engine])
  const audioReplacementInput = useRef<HTMLInputElement>(null)
  const [editing, setEditing] = useState<Track | null>(null)
  const settings = useSongSettingsSave(() => setEditing(null))
  const { save } = settings
  const [selectedLyricsID, setSelectedLyricsID] = useState<number | null>(null)
  const [position, setPosition] = useState(player.position)
  const [showTrash, setShowTrash] = useState(false)
  const [mobileView, setMobileView] = useState<'songs' | 'lyrics'>('songs')
  const [showQuota, setShowQuota] = useState(false)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const beforeMute = useRef(1)
  const [uploadAnchor, setUploadAnchor] = useState<HTMLDivElement | null>(null)
  const [sidebarUploadAnchor, setSidebarUploadAnchor] = useState<HTMLDivElement | null>(null)
  const quotaRef = useRef<HTMLDivElement>(null)
  const [sortBy, setSortBy] = useState<'uploadedAt' | 'title'>('uploadedAt')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('desc')
  const [showSort, setShowSort] = useState(false)
  const [selectedTrackIDs, setSelectedTrackIDs] = useState<string[]>([])
  const [showTagDialog, setShowTagDialog] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const selectionBusy = deleting
  const [transferError, setTransferError] = useState('')
  const [transferMessage, setTransferMessage] = useState('')
  const visibleTracks = useMemo(() => sortTracks(active?.tracks ?? [], sortBy, sortDirection), [filteredTracks, sortBy, sortDirection])
  const selectedTracks = (active?.tracks ?? []).filter((track) => selectedTrackIDs.includes(track.id))
  const refresh = () => Promise.all([
    query.invalidateQueries({ queryKey: ['library'] }),
    query.invalidateQueries({ queryKey: ['quota'] }),
    query.invalidateQueries({ queryKey: ['trash'] }),
  ])
  const report = (reason: unknown) => window.alert(reason instanceof Error ? reason.message : String(reason))

  useEffect(() => { setSelectedTrackIDs([]); setShowTagDialog(false); setTransferError(''); setTransferMessage('') }, [player.tagSelection, showTrash])
  useEffect(() => {
    if (!library || player.tagSelection === undefined) return
    const valid = new Set(allTagIDs(library.tags))
    const next = player.tagSelection.filter((id) => valid.has(id))
    if (next.length !== player.tagSelection.length) player.setTagSelection(next)
  }, [library, player.tagSelection])

  useEffect(() => {
    if (!showQuota) return
    const close = (event: PointerEvent) => {
      if (!quotaRef.current?.contains(event.target as Node)) setShowQuota(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [showQuota])

  useEffect(() => {
    if (isLoading || error) return
    const previous = usePlayer.getState()
    previous.reconcile([{ id: previous.playlistID ?? 'library', name: '', isDefault: true, tracks }])
    const restored = usePlayer.getState()
    if (restored.position !== previous.position) {
      setPosition(restored.position)
      if (restored.trackID && restored.trackID === previous.trackID && restored.audioVersion === previous.audioVersion) engine.seek(restored.position)
    }
  }, [library, dataUpdatedAt, isLoading, error, engine])

  useEffect(() => watchSessionLifecycle(() => {
    const state = usePlayer.getState()
    if (state.trackID && current?.id === state.trackID) state.setPosition(engine.position())
    flushPlayerSession()
  }), [engine, current?.id])
  useEffect(() => {
    if (!current) return
    if (current.originalAudioUrl && current.originalContentType) engine.load(current.audioUrl, { url: current.originalAudioUrl, contentType: current.originalContentType })
    else engine.load(current.audioUrl)
    const start = current.playbackStartSeconds
    const end = current.playbackEndSeconds ?? current.durationSeconds
    const versionChanged = player.audioVersion !== current.audioVersion
    const target = !versionChanged && player.position >= start && player.position < end ? player.position : start
    if (versionChanged) player.setAudioVersion(current.audioVersion, target)
    engine.seek(target); setPosition(target)
  }, [engine, current?.id, current?.audioVersion, current?.audioUrl, current?.originalAudioUrl, current?.originalContentType])

  useEffect(() => {
    if (current) engine.setVolume(player.masterVolume * current.volumeRatio)
  }, [engine, player.masterVolume, current?.volumeRatio])

  useEffect(() => {
    if (isLoading || error) return
    if (!current) {
      engine.pause()
      if (player.playing) player.setPlaying(false)
      return
    }
    if (player.playing) engine.play().catch(() => player.setPlaying(false))
    else {
      engine.pause()
      player.setPosition(engine.position())
      flushPlayerSession()
    }
  }, [engine, current?.id, current?.audioVersion, current?.audioUrl, current?.originalAudioUrl, current?.originalContentType, player.playing, isLoading, error])

  const seek = (seconds: number) => { engine.seek(seconds); setPosition(seconds); player.setPosition(seconds); flushPlayerSession() }
  const playTrack = (track: Track) => player.selectTrack(track.id, advanceRecovery(player.recovery, track.id), track.audioVersion)
  const activateTrack = (track: Track) => current?.id === track.id ? player.setPlaying(true) : playTrack(track)
  const next = () => {
    if (!active) return
    const selected = selectNext(active.tracks, current?.id ?? null, player.recovery)
    if (selected) {
      playTrack(selected)
      if (selected.id === current?.id) {
        seek(selected.playbackStartSeconds)
        if (player.playing) void engine.play().catch(() => player.setPlaying(false))
      }
    }
    else player.setPlaying(false)
  }

  const togglePlayback = () => { if (current) player.setPlaying(!player.playing); else if (player.mode === 'random') next() }
  useMediaSession({
    track: current, playing: player.playing, audio: engine.element(),
    getPosition: () => engine.position(),
    play: () => player.setPlaying(true), pause: () => player.setPlaying(false), next, seek,
  })
  usePlaybackShortcuts({
    enabled: !isLoading && !error,
    togglePlayback, next,
    seekBy: (delta) => { if (current) seek(Math.max(current.playbackStartSeconds, Math.min(current.playbackEndSeconds ?? current.durationSeconds, engine.position() + delta))) },
    adjustVolume: (delta) => player.setMasterVolume(Math.max(0, Math.min(1, Math.round((usePlayer.getState().masterVolume + delta) * 100) / 100))),
    toggleMute: () => {
      const volume = usePlayer.getState().masterVolume
      if (volume > 0) { beforeMute.current = volume; player.setMasterVolume(0) }
      else player.setMasterVolume(beforeMute.current)
    },
    showHelp: () => setShowShortcuts(true),
  })

  useEffect(() => {
    const audio = engine.element()
    const onTime = () => {
      if (!current) return
      const end = current.playbackEndSeconds
      if (end !== null && audio.currentTime >= end) {
        if (player.mode === 'repeat-one' && filteredTracks.some((track) => track.id === current.id)) { audio.currentTime = current.playbackStartSeconds; void audio.play() }
        else next()
        return
      }
      setPosition(engine.position()); player.setPosition(engine.position())
    }
    const onEnded = () => player.mode === 'repeat-one' && current && filteredTracks.some((track) => track.id === current.id) ? (audio.currentTime = current.playbackStartSeconds, void audio.play()) : next()
    const onError = () => { if (audio.error) { player.setPlaying(false); void api.currentUser().catch(() => {}) } }
    audio.addEventListener('timeupdate', onTime); audio.addEventListener('ended', onEnded)
    audio.addEventListener('error', onError)
    return () => { audio.removeEventListener('timeupdate', onTime); audio.removeEventListener('ended', onEnded); audio.removeEventListener('error', onError) }
  }, [current?.id, current?.audioVersion, current?.playbackStartSeconds, current?.playbackEndSeconds, current?.durationSeconds, player.mode, library, player.tagSelection, player.recovery])

  const lyricsSearch = useMutation({ mutationFn: api.searchLyrics, onMutate: () => setSelectedLyricsID(null) })
  const removeSelected = async () => {
    if (!active || !selectedTracks.length || selectionBusy) return
    const songs = selectedTracks
    if (!window.confirm(`删除选中的 ${songs.length} 首歌曲？歌曲将进入回收站。`)) return
    setDeleting(true); setTransferError(''); setTransferMessage('')
    try {
      const results = await Promise.allSettled(songs.map((track) => api.deleteTrack(track.id)))
      const failed = songs.filter((_, index) => results[index].status === 'rejected')
      await refresh()
      setSelectedTrackIDs(failed.map((track) => track.id))
      if (failed.length) setTransferError(`已删除 ${songs.length - failed.length} 首，${failed.length} 首失败，请重试。`)
      else setTransferMessage(`已删除 ${songs.length} 首歌曲`)
    } catch (reason) { setTransferError(reason instanceof Error ? reason.message : '删除失败，请重试') }
    finally { setDeleting(false) }
  }

  const selectionControls = (<div className="playlist-transfer" data-selected={selectedTracks.length > 0}>{selectedTracks.length > 0 && <><button className="mobile-select-all" type="button" disabled={selectionBusy} onClick={() => setSelectedTrackIDs(selectedTracks.length === visibleTracks.length ? [] : visibleTracks.map((track) => track.id))}>{selectedTracks.length === visibleTracks.length ? "取消全选" : "全选"}</button><button type="button" disabled={!selectedTracks.length || selectionBusy} onClick={() => setShowTagDialog(true)}>设置标签</button><button className="delete-selected" type="button" disabled={!selectedTracks.length || selectionBusy} onClick={() => void removeSelected()}>{deleting ? '正在删除…' : '删除'}</button><span>已选 {selectedTracks.length} 首</span></>}{transferMessage && <span role="status">{transferMessage}</span>}</div>)

  if (isLoading) return <main className="center">正在载入音乐空间…</main>
  if (error) return <main className="center error">无法连接服务器：{error.message}</main>

  return <div className="shell">
    <aside>
      <TagPanel groups={groups} tags={tags} enabled={enabledTags} onChange={(ids) => { player.setTagSelection(ids); setShowTrash(false); setMobileView('songs') }} onSaved={refresh} selecting={selectedTracks.length > 0} selectionAnchor={setSelectionAnchor}>
      <label className="mobile-menu-sort">排序<select value={`${sortBy}:${sortDirection}`} onChange={(event) => { const [field, direction] = event.target.value.split(':'); setSortBy(field as typeof sortBy); setSortDirection(direction as typeof sortDirection) }}><option value="uploadedAt:desc">上传时间 · 逆序</option><option value="uploadedAt:asc">上传时间 · 正序</option><option value="title:asc">歌曲名 · 正序</option><option value="title:desc">歌曲名 · 逆序</option></select></label>
      <div className="sidebar-tools" role="group" aria-label="音乐库工具">
        <button className="sidebar-tool-button sidebar-upload-button" type="button" onClick={() => document.getElementById('music-upload')?.click()}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M12 16V3m-4 4 4-4 4 4M4 16v5h16v-5"/></svg><span>上传音乐</span></button><div className="sidebar-upload-activity-anchor" ref={setSidebarUploadAnchor}/>
        {onLogout && <button className="sidebar-tool-button logout-button" type="button" title="退出登录" aria-label="退出登录" disabled={logoutBusy} onClick={onLogout}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M10 4H5v16h5m4-12 4 4-4 4m-5-4h10"/></svg><span className="logout-label">退出登录</span></button>}
      <button className="sidebar-tool-button shortcuts-toggle" type="button" aria-label="快捷键" title="快捷键（?）" onClick={() => setShowShortcuts(true)}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><rect x="2" y="5" width="20" height="14" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 12h.01M10 12h.01M14 12h.01M18 12h.01M7 15h10"/></svg><span>快捷键</span></button>
      <button className={`sidebar-tool-button trash-toggle ${showTrash ? 'active' : ''}`} type="button" aria-label="回收站" aria-pressed={showTrash} title="回收站" onClick={() => { setShowTrash(true); setShowQuota(false); }}><svg className="trash-icon" aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7"/></svg><span className="trash-label">回收站</span>{trash.length > 0 && <span className="trash-count">{trash.length}</span>}</button>
      <div className="quota-disclosure" ref={quotaRef} data-open={showQuota} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setShowQuota(false) }} onKeyDown={(event) => { if (event.key === 'Escape') { setShowQuota(false); quotaRef.current?.querySelector('button')?.focus() } }}>
        <button className="quota-toggle" type="button" aria-label="存储空间" title="存储空间" aria-expanded={showQuota} aria-controls="storage-quota" onClick={() => { setShowQuota(!showQuota); }}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="5" width="18" height="14" rx="3"/><path d="M3 14h18M16 17h2"/></svg></button>
        <div id="storage-quota" className="quota" title={quota?.reservedBytes ? `含处理中预留 ${bytes(quota.reservedBytes)}` : undefined}><b>已用 {bytes((quota?.usedBytes ?? 0) + (quota?.reservedBytes ?? 0))} / {bytes(quota?.limitBytes)}</b><i><u style={{ width: `${Math.min(100, (((quota?.usedBytes ?? 0) + (quota?.reservedBytes ?? 0)) / (quota?.limitBytes || 1)) * 100)}%` }}/></i></div>
      </div>
      </div>
      </TagPanel>
    </aside>
    <UploadTasks anchor={compact ? uploadAnchor : sidebarUploadAnchor} lyricsView={mobileView === 'lyrics'}/>

    <main className={showTrash ? 'trash-main' : 'music-main'}>{showTrash ? <>
      <section className="hero"><div><small>音乐库维护</small><h1>回收站</h1><p>歌曲至少保留 3 天；到期后由服务器自动清理。</p></div></section>
      <section className="trash-list">{trash.map((track) => <article key={track.id}><div><strong>{track.title}</strong><small>{track.artist || '未知艺术家'} · 预计清理：{track.purgeEligibleAt ? new Date(track.purgeEligibleAt).toLocaleString() : '待定'}</small>{track.purgeError && <small className="error">上次清理失败，服务器会重试</small>}</div><button onClick={() => api.restoreTrack(track.id).then(refresh).catch(report)}>恢复</button></article>)}{!trash.length && <div className="empty"><b>回收站是空的</b></div>}</section>
    </> : <>
      <div className="music-workspace" data-view={mobileView}>
      <div className="library-pane">
      <section className="playlist-toolbar" aria-label="音乐库操作">{compact && selectedTracks.length > 0 && selectionAnchor ? createPortal(selectionControls, selectionAnchor) : selectionControls}{compact && <div className="upload-actions"><div className="upload-activity-anchor" ref={setUploadAnchor}/></div>}</section>
      {!showTagDialog && transferError && <p className="selection-error error" role="alert">{transferError}</p>}
      <div className="list-scroll" role="region" aria-label="歌曲列表" tabIndex={0}>
      <section className="tracks"><div className="track heading"><input type="checkbox" aria-label="全选歌曲" disabled={!visibleTracks.length || selectionBusy} checked={visibleTracks.length > 0 && selectedTracks.length === visibleTracks.length} ref={(input) => { if (input) input.indeterminate = selectedTracks.length > 0 && selectedTracks.length < visibleTracks.length }} onChange={(event) => setSelectedTrackIDs(event.target.checked ? visibleTracks.map((track) => track.id) : [])}/><div className="track-heading-name"><span>歌曲</span></div><span>时长</span><div className="sort-tools" onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setShowSort(false) }} onKeyDown={(event) => { if (event.key === 'Escape') { setShowSort(false); event.currentTarget.querySelector('button')?.focus() } }}>
        <button className="sort-toggle" aria-label="排序" title="排序" aria-expanded={showSort} aria-controls="track-sort-options" onClick={() => setShowSort(!showSort)}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M4 6h12M4 12h8M4 18h4M18 10v10m-3-3 3 3 3-3"/></svg></button>
        {showSort && <div id="track-sort-options" className="sort-options" role="group" aria-label="排序选项">{(['uploadedAt', 'title'] as const).flatMap((field) => (['asc', 'desc'] as const).map((direction) => <button key={field + direction} aria-pressed={sortBy === field && sortDirection === direction} onClick={() => { setSortBy(field); setSortDirection(direction); setShowSort(false) }}>{field === 'uploadedAt' ? '上传时间' : '歌曲名'} · {direction === 'asc' ? '正序' : '逆序'}</button>))}</div>}
      </div></div>
        {visibleTracks.map((track) => <div className={`track ${current?.id === track.id ? 'playing' : ''}`} key={track.id} role="group" aria-label={`歌曲 ${track.title}`} tabIndex={0} onClick={(event) => { if (event.target instanceof Element && event.target.closest('input,button')) return; activateTrack(track) }} onKeyDown={(event) => { if (event.target === event.currentTarget && event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!event.repeat) activateTrack(track) } }}><div className="track-leading"><input type="checkbox" aria-label={`选择 ${track.title}`} disabled={selectionBusy} checked={selectedTrackIDs.includes(track.id)} onChange={(event) => setSelectedTrackIDs((ids) => event.target.checked ? [...ids, track.id] : ids.filter((id) => id !== track.id))}/></div><div className="track-info"><TrackCover src={track.coverUrl} className="track-cover"/><div className="track-meta"><strong title={track.title}>{track.title}</strong><div className="track-caption"><small>{trackSubtitle(track)}</small><TrackTagBadges tags={tags} ids={track.tagIds}/></div></div></div><span>{time(track.durationSeconds)}</span><div className="track-actions"><button className="edit" aria-label={`设置 ${track.title}`} title="歌曲设置" onClick={() => { settings.reset(); lyricsSearch.reset(); setSelectedLyricsID(null); setEditing({ ...track }) }}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3" fill="currentColor" stroke="none"/><circle cx="15" cy="17" r="3" fill="currentColor" stroke="none"/></svg></button></div></div>)}
        {!active?.tracks.length && <div className="empty"><b>{tracks.length ? '没有匹配的歌曲' : '这里还没有音乐'}</b><span>{tracks.length ? '开启其他标签，或调整歌曲标签' : '上传一个音频文件开始播放'}</span></div>}
      </section>
      </div></div>
      {current ? <LyricsView coverURL={current.coverUrl} viewKey={mobileView} trackTitle={current.title} lyrics={current.lyrics} layout={current.lyricsLayout} primaryLine={current.lyricsPrimaryLine} position={position} onSeek={seek}/> : <section className="lyrics-panel lyrics-placeholder" aria-label="播放歌词"><span>♫</span><p>选择一首音乐，查看歌词</p></section>}
      </div>
    </>}</main>

    {showTagDialog && <BulkTagDialog tags={tags} count={selectedTracks.length} onClose={() => setShowTagDialog(false)} onCreateTag={async (name) => {
      const existing = tags.find((tag) => tag.name.toLowerCase() === name.toLowerCase())
      if (existing) return existing
      const before = await api.library()
      const alreadyCreated = before.tags.find((tag) => tag.name.toLowerCase() === name.toLowerCase())
      if (alreadyCreated) { query.setQueryData(['library'], before); return alreadyCreated }
      await api.saveTag(name)
      const data = await api.library()
      query.setQueryData(['library'], data)
      const tag = data.tags.find((item) => item.name.toLowerCase() === name.toLowerCase())
      if (!tag) throw new Error('标签已创建，暂时无法读取，请刷新后重试')
      return tag
    }} onSave={async (ids, mode) => { await api.setTrackTags(selectedTracks.map((track) => track.id), ids, mode); await refresh(); setSelectedTrackIDs([]) }}/>}

    {editing && <div className="modal"><form className="song-settings" role="dialog" aria-modal="true" aria-labelledby="song-settings-title" onSubmit={(event) => { event.preventDefault(); if (!save.isPending && !settings.coverInvalid) save.mutate(editing) }}>
      <h2 id="song-settings-title">歌曲设置</h2>
      <fieldset className="settings-fields" disabled={save.isPending}>
      <div className="settings-identity"><label>歌曲名称<input required value={editing.title} onChange={(event) => setEditing({ ...editing, title: event.target.value })}/></label><label>歌手<input value={editing.artist} onChange={(event) => setEditing({ ...editing, artist: event.target.value })}/></label></div>
      <div className="settings-playback">
        <div className="settings-tuning"><label>随机权重<input type="number" min={Number.MIN_VALUE} step="any" value={editing.baseRandomWeight} onChange={(event) => setEditing({ ...editing, baseRandomWeight: Number(event.target.value) })}/></label><label>单曲音量 {Math.round(editing.volumeRatio * 100)}%<input type="range" min="0" max="1" step="0.01" value={editing.volumeRatio} onChange={(event) => setEditing({ ...editing, volumeRatio: Number(event.target.value) })}/></label></div>
        <div className="range"><label>开始（秒）<input type="number" min="0" step="0.1" value={editing.playbackStartSeconds} onChange={(event) => setEditing({ ...editing, playbackStartSeconds: Number(event.target.value) })}/></label><label>结束（秒）<input type="number" min="0" step="0.1" value={editing.playbackEndSeconds ?? ''} onChange={(event) => setEditing({ ...editing, playbackEndSeconds: event.target.value === '' ? null : Number(event.target.value) })}/></label></div>
      </div>
      <div className="settings-files">
      <CoverPicker key={editing.id} track={editing} file={settings.coverFile} onChange={settings.setCoverFile} onInvalidChange={settings.setCoverInvalid}/>
      <button className="settings-file-button settings-replace" type="button" onClick={() => audioReplacementInput.current?.click()}><span className="settings-file-icon" aria-hidden="true">♫</span><span>替换音频</span></button><input ref={audioReplacementInput} hidden type="file" aria-label="替换音频文件" accept={audioAccept} onChange={(event) => { const file = event.target.files?.[0]; if (file) settings.setAudioFile(file); event.target.value = '' }}/>
      {settings.audioFile && <div className="audio-replacement-draft"><span>待替换音频：{settings.audioFile.name}</span></div>}
      </div>
      <LyricsEditor key={editing.id} lyrics={editing.lyrics} layout={editing.lyricsLayout} primaryLine={editing.lyricsPrimaryLine}
        onTextChange={(text) => { setSelectedLyricsID(null); setEditing({ ...editing, lyrics: text }) }}
        onDisplayChange={(options) => setEditing({ ...editing, ...options })}
        actions={<button type="button" onClick={() => lyricsSearch.mutate(editing.id)} disabled={lyricsSearch.isPending}>{lyricsSearch.isPending ? '正在搜索…' : '从 LRCLIB 匹配'}</button>} searchResults={<>{lyricsSearch.error && <small className="error">{lyricsSearch.error.message}</small>}{lyricsSearch.data && <div className="lyrics-results"><small>使用“{lyricsSearch.data.queryTitle}”{lyricsSearch.data.queryArtist ? ` / “${lyricsSearch.data.queryArtist}”` : ''}找到 {lyricsSearch.data.candidates.length} 个候选{lyricsSearch.data.expandedSearch ? '（已补充仅歌名搜索）' : ''}</small>{lyricsSearch.data.candidates.map((candidate) => <button type="button" className={selectedLyricsID === candidate.id ? 'selected' : ''} key={candidate.id} onClick={() => { setSelectedLyricsID(candidate.id); setEditing({ ...editing, lyrics: candidate.lyrics }) }}><strong>{candidate.trackName}</strong><span>{candidate.artistName || '未知艺术家'} · {time(candidate.duration)} · {candidate.synced ? '同步歌词' : '纯文本'}</span></button>)}{!lyricsSearch.data.candidates.length && <span className="no-result">没有找到可用歌词</span>}</div>}</>}/>
      </fieldset>
      {settings.status && <small className="settings-save-status" role="status">{settings.status}</small>}
      {save.error && <p className="error" role="alert">{save.error.message}</p>}
      <div className="actions"><button type="button" disabled={save.isPending} onClick={() => { settings.reset(); setEditing(null) }}>取消</button><button className="primary" disabled={save.isPending || settings.coverInvalid}>{save.isPending ? '保存中…' : '保存'}</button></div>
    </form></div>}

    {showShortcuts && <ShortcutHelp onClose={() => setShowShortcuts(false)}/>}
    <footer className="player"><button className="now now-link" type="button" disabled={!current} aria-label={current ? `${!showTrash && mobileView === 'lyrics' ? '返回歌曲列表' : '查看歌词'}：${current.title}` : '选择一首音乐'} onClick={() => { setMobileView(!showTrash && mobileView === 'lyrics' ? 'songs' : 'lyrics'); setShowTrash(false) }}><TrackCover src={current?.coverUrl}/><span className="now-meta"><strong>{current?.title ?? '选择一首音乐'}</strong><small>{current?.artist || '尚未播放'}</small></span></button><div className="controls"><div><FloatingLyrics track={current} position={position} playing={player.playing} onToggle={togglePlayback} onNext={next}/><button onClick={() => player.setMode(player.mode === 'random' ? 'repeat-one' : 'random')}>{player.mode === 'random' ? '⤨' : '↻'}</button><button className="main" aria-label={player.playing ? '暂停' : '播放'} disabled={!current && (player.mode !== 'random' || !active?.tracks.length)} onClick={togglePlayback}>{player.playing ? 'Ⅱ' : '▶'}</button><button disabled={!active?.tracks.length} onClick={next}>▶|</button></div><label><span>{time(position)}</span><input type="range" min={current?.playbackStartSeconds ?? 0} max={current?.playbackEndSeconds ?? current?.durationSeconds ?? 1} step="0.1" value={position} onChange={(event) => { seek(Number(event.target.value)) }}/><span>{time(current?.playbackEndSeconds ?? current?.durationSeconds)}</span></label></div><label className="volume">♪<input type="range" min="0" max="1" step="0.01" value={player.masterVolume} onChange={(event) => player.setMasterVolume(Number(event.target.value))}/></label></footer>
  </div>
}
