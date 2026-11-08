import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Track } from '@music/domain'
import { advanceRecovery, selectNext, sortTracks } from '@music/player-policy'
import { api } from './api'
import { UploadTasks, audioAccept } from './UploadTasks'
import { LyricsView } from './LyricsView'
import { LyricsDisplaySettings } from './LyricsDisplaySettings'
import { WebAudioEngine } from './platform/audio'
import { usePlayer } from './player-store'

const bytes = (value = 0) => value >= 1024 ** 3 ? `${Number((value / 1024 ** 3).toFixed(1))} GiB` : `${(value / 1024 ** 2).toFixed(1)} MiB`
const time = (value = 0) => `${Math.floor(value / 60)}:${Math.floor(value % 60).toString().padStart(2, '0')}`

const trackSubtitle = (track: Track) => {
  const album = track.album?.trim() ?? ''
  const normalized = (name: string) => name.trim().normalize('NFKC').toLowerCase()
  return (track.artist || '未知艺术家') + (album && normalized(album) !== normalized(track.title) ? ' · ' + album : '')
}

export function App() {
  const query = useQueryClient()
  const { data: playlists = [], isLoading, error } = useQuery({ queryKey: ['playlists'], queryFn: api.playlists })
  const { data: quota } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: trash = [] } = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const player = usePlayer()
  const active = playlists.find((playlist) => playlist.id === player.playlistID) ?? playlists[0]
  const current = active?.tracks.find((track) => track.id === player.trackID) ?? null
  const [engine] = useState(() => new WebAudioEngine())
  const [editing, setEditing] = useState<Track | null>(null)
  const [selectedLyricsID, setSelectedLyricsID] = useState<number | null>(null)
  const [position, setPosition] = useState(player.position)
  const [playlistName, setPlaylistName] = useState('')
  const [showTrash, setShowTrash] = useState(false)
  const [sortBy, setSortBy] = useState<'uploadedAt' | 'fileName'>('uploadedAt')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('desc')
  const [showSort, setShowSort] = useState(false)
  const visibleTracks = useMemo(() => sortTracks(active?.tracks ?? [], sortBy, sortDirection), [active?.tracks, sortBy, sortDirection])
  const library = useMemo(() => playlists.find((playlist) => playlist.isDefault)?.tracks ?? [], [playlists])
  const refresh = () => Promise.all([
    query.invalidateQueries({ queryKey: ['playlists'] }),
    query.invalidateQueries({ queryKey: ['quota'] }),
    query.invalidateQueries({ queryKey: ['trash'] }),
  ])
  const report = (reason: unknown) => window.alert(reason instanceof Error ? reason.message : String(reason))

  useEffect(() => { if (active && !player.playlistID) player.selectPlaylist(active.id) }, [active, player])
  useEffect(() => {
    if (!current) return
    if (current.originalAudioUrl && current.originalContentType) engine.load(current.audioUrl, { url: current.originalAudioUrl, contentType: current.originalContentType })
    else engine.load(current.audioUrl)
    const start = current.playbackStartSeconds
    const end = current.playbackEndSeconds ?? current.durationSeconds
    const versionChanged = player.audioVersion !== current.audioVersion
    const target = !versionChanged && player.position >= start && player.position < end ? player.position : start
    if (versionChanged) player.setAudioVersion(current.audioVersion)
    engine.seek(target); setPosition(target)
  }, [engine, current?.id, current?.audioVersion, current?.audioUrl, current?.originalAudioUrl, current?.originalContentType])

  useEffect(() => {
    if (current) engine.setVolume(player.masterVolume * current.volumeRatio)
  }, [engine, player.masterVolume, current?.volumeRatio])

  useEffect(() => {
    if (!current) {
      engine.pause()
      if (player.playing) player.setPlaying(false)
      return
    }
    if (player.playing) engine.play().catch(() => player.setPlaying(false))
    else engine.pause()
  }, [engine, current?.id, current?.audioVersion, current?.audioUrl, current?.originalAudioUrl, current?.originalContentType, player.playing])

  const playTrack = (track: Track) => player.selectTrack(track.id, advanceRecovery(player.recovery, track.id), track.audioVersion)
  const next = () => {
    if (!active) return
    const selected = selectNext(active.tracks, current?.id ?? null, player.recovery)
    if (selected) playTrack(selected)
  }

  useEffect(() => {
    const audio = engine.element()
    const onTime = () => {
      if (!current) return
      const end = current.playbackEndSeconds
      if (end !== null && audio.currentTime >= end) {
        if (player.mode === 'repeat-one') { audio.currentTime = current.playbackStartSeconds; void audio.play() }
        else next()
        return
      }
      setPosition(audio.currentTime); player.setPosition(audio.currentTime)
    }
    const onEnded = () => player.mode === 'repeat-one' && current ? (audio.currentTime = current.playbackStartSeconds, void audio.play()) : next()
    const onError = () => { if (audio.error) player.setPlaying(false) }
    audio.addEventListener('timeupdate', onTime); audio.addEventListener('ended', onEnded)
    audio.addEventListener('error', onError)
    return () => { audio.removeEventListener('timeupdate', onTime); audio.removeEventListener('ended', onEnded); audio.removeEventListener('error', onError) }
  }, [current?.id, current?.audioVersion, current?.playbackStartSeconds, current?.playbackEndSeconds, current?.durationSeconds, player.mode, active?.id, player.recovery])

  const create = useMutation({ mutationFn: api.createPlaylist, onSuccess: refresh })
  const save = useMutation({ mutationFn: api.updateTrack, onSuccess: async () => { await refresh(); setEditing(null) } })
  const lyricsSearch = useMutation({ mutationFn: api.searchLyrics, onMutate: () => setSelectedLyricsID(null) })
  const remove = async (track: Track) => {
    if (!active) return
    const onlyDefault = active.isDefault && playlists.filter((playlist) => playlist.tracks.some((item) => item.id === track.id)).length === 1
    if (onlyDefault && !window.confirm('这首歌只属于默认歌单，移除后将进入回收站。是否继续？')) return
    try { await api.removeTrack(active.id, track.id, onlyDefault); await refresh() } catch (reason) { report(reason) }
  }

  if (isLoading) return <main className="center">正在载入音乐空间…</main>
  if (error) return <main className="center error">无法连接服务器：{error.message}</main>

  return <div className="shell">
    <aside>
      <header><span className="mark">♫</span><div><strong>音乐空间</strong><small>你的私人播放站</small></div></header>
      <nav>{playlists.map((playlist) => <button className={!showTrash && playlist.id === active?.id ? 'active' : ''} key={playlist.id} onClick={() => { setShowTrash(false); player.selectPlaylist(playlist.id) }}><span>{playlist.isDefault ? '♥' : '≡'}</span>{playlist.name}<em>{playlist.tracks.length}</em></button>)}<button className={showTrash ? 'active' : ''} onClick={() => setShowTrash(true)}><span>♻</span>回收站<em>{trash.length}</em></button></nav>
      <form className="new-list" onSubmit={(event) => { event.preventDefault(); if (playlistName.trim()) { create.mutate(playlistName.trim()); setPlaylistName('') } }}><input value={playlistName} onChange={(event) => setPlaylistName(event.target.value)} placeholder="新建歌单"/><button>＋</button></form>
      <div className="sidebar-status">
        <UploadTasks/>
      <div className="quota" title={quota?.reservedBytes ? `含处理中预留 ${bytes(quota.reservedBytes)}` : undefined}><b>已用 {bytes((quota?.usedBytes ?? 0) + (quota?.reservedBytes ?? 0))} / {bytes(quota?.limitBytes)}</b><i><u style={{ width: `${Math.min(100, (((quota?.usedBytes ?? 0) + (quota?.reservedBytes ?? 0)) / (quota?.limitBytes || 1)) * 100)}%` }}/></i></div>
      </div>
    </aside>

    <main>{showTrash ? <>
      <section className="hero"><div><small>音乐库维护</small><h1>回收站</h1><p>歌曲至少保留 3 天；到期后由服务器自动清理。</p></div></section>
      <section className="trash-list">{trash.map((track) => <article key={track.id}><div><strong>{track.title}</strong><small>{track.artist || '未知艺术家'} · 预计清理：{track.purgeEligibleAt ? new Date(track.purgeEligibleAt).toLocaleString() : '待定'}</small>{track.purgeError && <small className="error">上次清理失败，服务器会重试</small>}</div><button onClick={() => api.restoreTrack(track.id).then(refresh).catch(report)}>恢复</button></article>)}{!trash.length && <div className="empty"><b>回收站是空的</b></div>}</section>
    </> : <>
      <section className="hero"><div><small>当前歌单</small><div className="playlist-title"><h1>{active?.name ?? '默认歌单'}</h1><span className="track-count">{active?.tracks.length ?? 0} 首音乐</span></div>{active && !active.isDefault && <span className="playlist-actions"><button onClick={async () => { const name = window.prompt('歌单名称', active.name)?.trim(); if (name) { try { await api.renamePlaylist(active.id, name); await refresh() } catch (reason) { report(reason) } } }}>重命名</button><button onClick={async () => { if (!window.confirm(`删除歌单“${active.name}”？歌曲本身不会被删除。`)) return; try { await api.deletePlaylist(active.id); player.selectPlaylist(playlists.find((playlist) => playlist.isDefault)?.id ?? ''); await refresh() } catch (reason) { report(reason) } }}>删除歌单</button></span>}</div><label className="upload" htmlFor="music-upload">上传音乐</label></section>
      <section className="tracks"><div className="track heading"><span>#</span><span>歌曲</span><span>时长</span><div className="sort-tools" onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setShowSort(false) }} onKeyDown={(event) => { if (event.key === 'Escape') { setShowSort(false); event.currentTarget.querySelector('button')?.focus() } }}>
        <button className="sort-toggle" aria-label="排序" title="排序" aria-expanded={showSort} aria-controls="track-sort-options" onClick={() => setShowSort(!showSort)}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M4 6h12M4 12h8M4 18h4M18 10v10m-3-3 3 3 3-3"/></svg></button>
        {showSort && <div id="track-sort-options" className="sort-options" role="group" aria-label="排序选项">{(['uploadedAt', 'fileName'] as const).flatMap((field) => (['asc', 'desc'] as const).map((direction) => <button key={field + direction} aria-pressed={sortBy === field && sortDirection === direction} onClick={() => { setSortBy(field); setSortDirection(direction); setShowSort(false) }}>{field === 'uploadedAt' ? '上传时间' : '文件名'} · {direction === 'asc' ? '正序' : '逆序'}</button>))}</div>}
      </div></div>
        {visibleTracks.map((track) => <div className={`track ${current?.id === track.id ? 'playing' : ''}`} key={track.id}><button className="play" onClick={() => current?.id === track.id && player.playing ? player.setPlaying(false) : playTrack(track)}>{current?.id === track.id && player.playing ? 'Ⅱ' : '▶'}</button><div><strong title={track.title}>{track.title}</strong><small>{trackSubtitle(track)}</small></div><span>{time(track.durationSeconds)}</span><div className="track-actions"><button className="remove-track" aria-label={`从歌单移除 ${track.title}`} title="从歌单移除" onClick={() => void remove(track)}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M3 6h18M9 6V4h6v2M5 6l1 14h12l1-14M10 10v6m4-6v6"/></svg></button><button className="edit" aria-label={`设置 ${track.title}`} title="歌曲设置" onClick={() => { lyricsSearch.reset(); setSelectedLyricsID(null); setEditing({ ...track }) }}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3" fill="currentColor" stroke="none"/><circle cx="15" cy="17" r="3" fill="currentColor" stroke="none"/></svg></button></div></div>)}
        {!active?.tracks.length && <div className="empty"><b>这里还没有音乐</b><span>上传一个音频文件开始播放</span></div>}
      </section>
      {active && !active.isDefault && library.length > 0 && <details className="add"><summary>从音乐库添加歌曲</summary>{library.filter((track) => !active.tracks.some((item) => item.id === track.id)).map((track) => <button key={track.id} onClick={() => api.addTrack(active.id, track.id).then(refresh).catch(report)}>＋ {track.title}</button>)}</details>}
      <LyricsView trackTitle={current?.title} lyrics={current?.lyrics ?? ''} layout={current?.lyricsLayout} primaryLine={current?.lyricsPrimaryLine} position={position} onSeek={(seconds) => { engine.seek(seconds); setPosition(seconds); player.setPosition(seconds) }}/>
    </>}</main>

    {editing && <div className="modal"><form className="song-settings" role="dialog" aria-modal="true" aria-labelledby="song-settings-title" onSubmit={(event) => { event.preventDefault(); save.mutate(editing) }}>
      <h2 id="song-settings-title">歌曲设置</h2>
      <div className="settings-identity"><label>歌曲名称<input required value={editing.title} onChange={(event) => setEditing({ ...editing, title: event.target.value })}/></label><label>歌手<input value={editing.artist} onChange={(event) => setEditing({ ...editing, artist: event.target.value })}/></label></div>
      <div className="settings-playback">
        <div className="settings-tuning"><label>随机权重<input type="number" min={Number.MIN_VALUE} step="any" value={editing.baseRandomWeight} onChange={(event) => setEditing({ ...editing, baseRandomWeight: Number(event.target.value) })}/></label><label>单曲音量 {Math.round(editing.volumeRatio * 100)}%<input type="range" min="0" max="1" step="0.01" value={editing.volumeRatio} onChange={(event) => setEditing({ ...editing, volumeRatio: Number(event.target.value) })}/></label></div>
        <div className="range"><label>开始（秒）<input type="number" min="0" step="0.1" value={editing.playbackStartSeconds} onChange={(event) => setEditing({ ...editing, playbackStartSeconds: Number(event.target.value) })}/></label><label>结束（秒）<input type="number" min="0" step="0.1" value={editing.playbackEndSeconds ?? ''} onChange={(event) => setEditing({ ...editing, playbackEndSeconds: event.target.value === '' ? null : Number(event.target.value) })}/></label></div>
      </div>
      <details className="settings-replace"><summary>替换音频文件</summary><input type="file" aria-label="替换音频文件" accept={audioAccept} onChange={async (event) => { const file = event.target.files?.[0]; if (!file) return; try { await api.replaceAudio(editing.id, file); setEditing(null); await query.invalidateQueries({ queryKey: ['jobs'] }); await refresh() } catch (reason) { report(reason) } event.target.value = '' }}/></details>
      <div className="lyrics-editor"><div className="lyrics-tools"><span>歌词</span><button type="button" onClick={() => lyricsSearch.mutate(editing.id)} disabled={lyricsSearch.isPending}>{lyricsSearch.isPending ? '正在搜索…' : '从 LRCLIB 匹配'}</button></div>{lyricsSearch.error && <small className="error">{lyricsSearch.error.message}</small>}{lyricsSearch.data && <div className="lyrics-results"><small>使用“{lyricsSearch.data.queryTitle}”{lyricsSearch.data.queryArtist ? ` / “${lyricsSearch.data.queryArtist}”` : ''}找到 {lyricsSearch.data.candidates.length} 个候选{lyricsSearch.data.expandedSearch ? '（已补充仅歌名搜索）' : ''}</small>{lyricsSearch.data.candidates.map((candidate) => <button type="button" className={selectedLyricsID === candidate.id ? 'selected' : ''} key={candidate.id} onClick={() => { setSelectedLyricsID(candidate.id); setEditing({ ...editing, lyrics: candidate.lyrics }) }}><strong>{candidate.trackName}</strong><span>{candidate.artistName || '未知艺术家'} · {time(candidate.duration)} · {candidate.synced ? '同步歌词' : '纯文本'}</span></button>)}{!lyricsSearch.data.candidates.length && <span className="no-result">没有找到可用歌词</span>}</div>}<textarea aria-label="歌词" rows={6} value={editing.lyrics} onChange={(event) => { setSelectedLyricsID(null); setEditing({ ...editing, lyrics: event.target.value }) }}/><LyricsDisplaySettings lyrics={editing.lyrics} layout={editing.lyricsLayout} primaryLine={editing.lyricsPrimaryLine} onChange={(options) => setEditing({ ...editing, ...options })}/></div>
      {save.error && <p className="error">{save.error.message}</p>}
      <div className="actions"><button className="danger" type="button" onClick={async () => { if (!window.confirm(`将“${editing.title}”移入回收站？`)) return; try { await api.deleteTrack(editing.id); setEditing(null); await refresh() } catch (reason) { report(reason) } }}>移入回收站</button><span/><button type="button" onClick={() => setEditing(null)}>取消</button><button className="primary">保存</button></div>
    </form></div>}

    <footer className="player"><div className="now"><span className="cover">♫</span><div><strong>{current?.title ?? '选择一首音乐'}</strong><small>{current?.artist || '尚未播放'}</small></div></div><div className="controls"><div><button onClick={() => player.setMode(player.mode === 'random' ? 'repeat-one' : 'random')}>{player.mode === 'random' ? '⤨' : '↻'}</button><button className="main" disabled={!current} onClick={() => player.setPlaying(!player.playing)}>{player.playing ? 'Ⅱ' : '▶'}</button><button disabled={!active?.tracks.length} onClick={next}>▶|</button></div><label><span>{time(position)}</span><input type="range" min={current?.playbackStartSeconds ?? 0} max={current?.playbackEndSeconds ?? current?.durationSeconds ?? 1} step="0.1" value={position} onChange={(event) => { const nextPosition = Number(event.target.value); setPosition(nextPosition); engine.seek(nextPosition); player.setPosition(nextPosition) }}/><span>{time(current?.playbackEndSeconds ?? current?.durationSeconds)}</span></label></div><label className="volume">♪<input type="range" min="0" max="1" step="0.01" value={player.masterVolume} onChange={(event) => player.setMasterVolume(Number(event.target.value))}/></label></footer>
  </div>
}
