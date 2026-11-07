import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Track } from '@music/domain'
import { advanceRecovery, selectNext } from '@music/player-policy'
import { api } from './api'
import { LyricsView } from './LyricsView'
import { WebAudioEngine } from './platform/audio'
import { usePlayer } from './player-store'

const bytes = (value = 0) => `${(value / 1024 / 1024).toFixed(1)} MB`
const time = (value = 0) => `${Math.floor(value / 60)}:${Math.floor(value % 60).toString().padStart(2, '0')}`
const audioAccept = 'audio/*,.aac,.m4a,.m4b,.mp4,.flac,.ogg,.opus,.wma,.wav'

export function App() {
  const query = useQueryClient()
  const { data: playlists = [], isLoading, error } = useQuery({ queryKey: ['playlists'], queryFn: api.playlists })
  const { data: quota } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const { data: trash = [] } = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const player = usePlayer()
  const active = playlists.find((playlist) => playlist.id === player.playlistID) ?? playlists[0]
  const current = active?.tracks.find((track) => track.id === player.trackID) ?? null
  const engine = useRef(new WebAudioEngine()).current
  const [editing, setEditing] = useState<Track | null>(null)
  const [selectedLyricsID, setSelectedLyricsID] = useState<number | null>(null)
  const [position, setPosition] = useState(player.position)
  const [playlistName, setPlaylistName] = useState('')
  const [showTrash, setShowTrash] = useState(false)
  const library = useMemo(() => playlists.find((playlist) => playlist.isDefault)?.tracks ?? [], [playlists])
  const refresh = () => Promise.all([
    query.invalidateQueries({ queryKey: ['playlists'] }),
    query.invalidateQueries({ queryKey: ['quota'] }),
    query.invalidateQueries({ queryKey: ['trash'] }),
  ])
  const report = (reason: unknown) => window.alert(reason instanceof Error ? reason.message : String(reason))

  useEffect(() => { if (active && !player.playlistID) player.selectPlaylist(active.id) }, [active, player])
  useEffect(() => {
    if (!current) {
      engine.pause()
      if (player.playing) player.setPlaying(false)
      return
    }
    engine.load(current.audioUrl)
    engine.setVolume(player.masterVolume * current.volumeRatio)
    const start = current.playbackStartSeconds
    const end = current.playbackEndSeconds ?? current.durationSeconds
    const versionChanged = player.audioVersion !== current.audioVersion
    const target = !versionChanged && player.position >= start && player.position < end ? player.position : start
    if (versionChanged) player.setAudioVersion(current.audioVersion)
    engine.seek(target); setPosition(target)
    if (player.playing) engine.play().catch(() => player.setPlaying(false))
    else engine.pause()
  }, [current?.id, current?.audioVersion, player.playing, player.masterVolume, current?.volumeRatio])

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
      const end = current.playbackEndSeconds ?? current.durationSeconds
      if (audio.currentTime >= end) {
        if (player.mode === 'repeat-one') { audio.currentTime = current.playbackStartSeconds; void audio.play() }
        else next()
        return
      }
      setPosition(audio.currentTime); player.setPosition(audio.currentTime)
    }
    const onEnded = () => player.mode === 'repeat-one' && current ? (audio.currentTime = current.playbackStartSeconds, void audio.play()) : next()
    audio.addEventListener('timeupdate', onTime); audio.addEventListener('ended', onEnded)
    return () => { audio.removeEventListener('timeupdate', onTime); audio.removeEventListener('ended', onEnded) }
  }, [current?.id, player.mode, active?.id, player.recovery])

  const upload = useMutation({ mutationFn: api.upload, onSuccess: refresh })
  const create = useMutation({ mutationFn: api.createPlaylist, onSuccess: refresh })
  const save = useMutation({ mutationFn: api.updateTrack, onSuccess: async () => { await refresh(); setEditing(null) } })
  const lyricsSearch = useMutation({ mutationFn: api.searchLyrics, onMutate: () => setSelectedLyricsID(null) })
  const remove = async (track: Track) => {
    if (!active) return
    const onlyDefault = active.isDefault && playlists.filter((playlist) => playlist.tracks.some((item) => item.id === track.id)).length === 1
    if (onlyDefault && !window.confirm('这首歌只属于默认歌单，移除后将进入回收站。是否继续？')) return
    try { await api.removeTrack(active.id, track.id, onlyDefault); await refresh() } catch (reason) { report(reason) }
  }
  const reorder = async (index: number, offset: number) => {
    if (!active || index + offset < 0 || index + offset >= active.tracks.length) return
    const ids = active.tracks.map((track) => track.id)
    ;[ids[index], ids[index + offset]] = [ids[index + offset], ids[index]]
    try { await api.reorderPlaylist(active.id, ids); await refresh() } catch (reason) { report(reason) }
  }

  if (isLoading) return <main className="center">正在载入音乐空间…</main>
  if (error) return <main className="center error">无法连接服务器：{error.message}</main>

  return <div className="shell">
    <aside>
      <header><span className="mark">♫</span><div><strong>音乐空间</strong><small>你的私人播放站</small></div></header>
      <nav>{playlists.map((playlist) => <button className={!showTrash && playlist.id === active?.id ? 'active' : ''} key={playlist.id} onClick={() => { setShowTrash(false); player.selectPlaylist(playlist.id) }}><span>{playlist.isDefault ? '♥' : '≡'}</span>{playlist.name}<em>{playlist.tracks.length}</em></button>)}<button className={showTrash ? 'active' : ''} onClick={() => setShowTrash(true)}><span>♻</span>回收站<em>{trash.length}</em></button></nav>
      <form className="new-list" onSubmit={(event) => { event.preventDefault(); if (playlistName.trim()) { create.mutate(playlistName.trim()); setPlaylistName('') } }}><input value={playlistName} onChange={(event) => setPlaylistName(event.target.value)} placeholder="新建歌单"/><button>＋</button></form>
      <div className="quota"><span>存储空间</span><b>{bytes(quota?.usedBytes)} / {bytes(quota?.limitBytes)}</b><i><u style={{ width: `${Math.min(100, ((quota?.usedBytes ?? 0) / (quota?.limitBytes || 1)) * 100)}%` }}/></i></div>
    </aside>

    <main>{showTrash ? <>
      <section className="hero"><div><small>音乐库维护</small><h1>回收站</h1><p>歌曲至少保留 3 天；到期后由服务器自动清理。</p></div></section>
      <section className="trash-list">{trash.map((track) => <article key={track.id}><div><strong>{track.title}</strong><small>{track.artist || '未知艺术家'} · 预计清理：{track.purgeEligibleAt ? new Date(track.purgeEligibleAt).toLocaleString() : '待定'}</small>{track.purgeError && <small className="error">上次清理失败，服务器会重试</small>}</div><button onClick={() => api.restoreTrack(track.id).then(refresh).catch(report)}>恢复</button></article>)}{!trash.length && <div className="empty"><b>回收站是空的</b></div>}</section>
    </> : <>
      <section className="hero"><div><small>当前歌单</small><h1>{active?.name ?? '默认歌单'}</h1><p>{active?.tracks.length ?? 0} 首音乐 · 加权随机播放</p>{active && !active.isDefault && <span className="playlist-actions"><button onClick={async () => { const name = window.prompt('歌单名称', active.name)?.trim(); if (name) { try { await api.renamePlaylist(active.id, name); await refresh() } catch (reason) { report(reason) } } }}>重命名</button><button onClick={async () => { if (!window.confirm(`删除歌单“${active.name}”？歌曲本身不会被删除。`)) return; try { await api.deletePlaylist(active.id); player.selectPlaylist(playlists.find((playlist) => playlist.isDefault)?.id ?? ''); await refresh() } catch (reason) { report(reason) } }}>删除歌单</button></span>}</div><label className="upload"><input type="file" accept={audioAccept} onChange={(event) => { const file = event.target.files?.[0]; if (file) upload.mutate(file); event.target.value = '' }}/>{upload.isPending ? '处理中…' : '上传音乐'}</label></section>
      {upload.error && <p className="error">{upload.error.message}</p>}
      <section className="tracks"><div className="track heading"><span>#</span><span>歌曲</span><span>时长</span><span>权重</span><span>音量</span><span/></div>
        {active?.tracks.map((track, index) => <div className={`track ${current?.id === track.id ? 'playing' : ''}`} key={track.id}><button className="play" onClick={() => current?.id === track.id && player.playing ? player.setPlaying(false) : playTrack(track)}>{current?.id === track.id && player.playing ? 'Ⅱ' : '▶'}</button><div><strong>{track.title}</strong><small>{track.artist || '未知艺术家'}{track.album ? ` · ${track.album}` : ''}</small></div><span>{time(track.durationSeconds)}</span><span>{track.baseRandomWeight}</span><span>{Math.round(track.volumeRatio * 100)}%</span><div className="track-actions"><button aria-label={`上移 ${track.title}`} disabled={index === 0} onClick={() => void reorder(index, -1)}>↑</button><button aria-label={`下移 ${track.title}`} disabled={index === active.tracks.length - 1} onClick={() => void reorder(index, 1)}>↓</button><button aria-label={`从歌单移除 ${track.title}`} onClick={() => void remove(track)}>移除</button><button className="edit" onClick={() => { lyricsSearch.reset(); setSelectedLyricsID(null); setEditing({ ...track }) }}>设置</button></div></div>)}
        {!active?.tracks.length && <div className="empty"><b>这里还没有音乐</b><span>上传一个音频文件开始播放</span></div>}
      </section>
      {active && !active.isDefault && library.length > 0 && <details className="add"><summary>从音乐库添加歌曲</summary>{library.filter((track) => !active.tracks.some((item) => item.id === track.id)).map((track) => <button key={track.id} onClick={() => api.addTrack(active.id, track.id).then(refresh).catch(report)}>＋ {track.title}</button>)}</details>}
      <LyricsView trackTitle={current?.title} lyrics={current?.lyrics ?? ''} position={position} onSeek={(seconds) => { engine.seek(seconds); setPosition(seconds); player.setPosition(seconds) }}/>
    </>}</main>

    {editing && <div className="modal"><form onSubmit={(event) => { event.preventDefault(); save.mutate(editing) }}><h2>歌曲设置</h2><p>{editing.title}</p>
      <label>随机权重<input type="number" min={Number.MIN_VALUE} step="any" value={editing.baseRandomWeight} onChange={(event) => setEditing({ ...editing, baseRandomWeight: Number(event.target.value) })}/></label>
      <div className="range"><label>开始（秒）<input type="number" min="0" step="0.1" value={editing.playbackStartSeconds} onChange={(event) => setEditing({ ...editing, playbackStartSeconds: Number(event.target.value) })}/></label><label>结束（秒）<input type="number" min="0" step="0.1" value={editing.playbackEndSeconds ?? ''} onChange={(event) => setEditing({ ...editing, playbackEndSeconds: event.target.value === '' ? null : Number(event.target.value) })}/></label></div>
      <label>单曲音量 {Math.round(editing.volumeRatio * 100)}%<input type="range" min="0" max="1" step="0.01" value={editing.volumeRatio} onChange={(event) => setEditing({ ...editing, volumeRatio: Number(event.target.value) })}/></label>
      <label className="replace">替换音频文件<input type="file" accept={audioAccept} onChange={async (event) => { const file = event.target.files?.[0]; if (!file) return; try { const replaced = await api.replaceAudio(editing.id, file); setEditing({ ...replaced }); await refresh() } catch (reason) { report(reason) } event.target.value = '' }}/></label>
      <div className="lyrics-editor"><div className="lyrics-tools"><span>歌词</span><button type="button" onClick={() => lyricsSearch.mutate(editing.id)} disabled={lyricsSearch.isPending}>{lyricsSearch.isPending ? '正在搜索…' : '从 LRCLIB 匹配'}</button></div>{lyricsSearch.error && <small className="error">{lyricsSearch.error.message}</small>}{lyricsSearch.data && <div className="lyrics-results"><small>使用“{lyricsSearch.data.queryTitle}”{lyricsSearch.data.queryArtist ? ` / “${lyricsSearch.data.queryArtist}”` : ''}找到 {lyricsSearch.data.candidates.length} 个候选</small>{lyricsSearch.data.candidates.map((candidate) => <button type="button" className={selectedLyricsID === candidate.id ? 'selected' : ''} key={candidate.id} onClick={() => { setSelectedLyricsID(candidate.id); setEditing({ ...editing, lyrics: candidate.lyrics }) }}><strong>{candidate.trackName}</strong><span>{candidate.artistName || '未知艺术家'} · {time(candidate.duration)} · {candidate.synced ? '同步歌词' : '纯文本'}</span></button>)}{!lyricsSearch.data.candidates.length && <span className="no-result">没有找到可用歌词</span>}</div>}<textarea aria-label="歌词" rows={8} value={editing.lyrics} onChange={(event) => { setSelectedLyricsID(null); setEditing({ ...editing, lyrics: event.target.value }) }}/></div>
      <div className="actions"><button className="danger" type="button" onClick={async () => { if (!window.confirm(`将“${editing.title}”移入回收站？`)) return; try { await api.deleteTrack(editing.id); setEditing(null); await refresh() } catch (reason) { report(reason) } }}>移入回收站</button><span/><button type="button" onClick={() => setEditing(null)}>取消</button><button className="primary">保存</button></div>
    </form></div>}

    <footer className="player"><div className="now"><span className="cover">♫</span><div><strong>{current?.title ?? '选择一首音乐'}</strong><small>{current?.artist || '尚未播放'}</small></div></div><div className="controls"><div><button onClick={() => player.setMode(player.mode === 'random' ? 'repeat-one' : 'random')}>{player.mode === 'random' ? '⤨' : '↻'}</button><button className="main" disabled={!current} onClick={() => player.setPlaying(!player.playing)}>{player.playing ? 'Ⅱ' : '▶'}</button><button disabled={!active?.tracks.length} onClick={next}>▶|</button></div><label><span>{time(position)}</span><input type="range" min={current?.playbackStartSeconds ?? 0} max={current?.playbackEndSeconds ?? current?.durationSeconds ?? 1} step="0.1" value={position} onChange={(event) => { const nextPosition = Number(event.target.value); setPosition(nextPosition); engine.seek(nextPosition); player.setPosition(nextPosition) }}/><span>{time(current?.playbackEndSeconds ?? current?.durationSeconds)}</span></label></div><label className="volume">♪<input type="range" min="0" max="1" step="0.01" value={player.masterVolume} onChange={(event) => player.setMasterVolume(Number(event.target.value))}/></label></footer>
  </div>
}
