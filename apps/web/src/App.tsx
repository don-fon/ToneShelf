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

export function App() {
  const query = useQueryClient()
  const { data: playlists = [], isLoading, error } = useQuery({ queryKey: ['playlists'], queryFn: api.playlists })
  const { data: quota } = useQuery({ queryKey: ['quota'], queryFn: api.quota })
  const player = usePlayer()
  const active = playlists.find((p) => p.id === player.playlistID) ?? playlists[0]
  const current = active?.tracks.find((t) => t.id === player.trackID) ?? null
  const engine = useRef(new WebAudioEngine()).current
  const [editing, setEditing] = useState<Track | null>(null)
  const [selectedLyricsID, setSelectedLyricsID] = useState<number | null>(null)
  const [position, setPosition] = useState(player.position)
  const refresh = () => Promise.all([query.invalidateQueries({ queryKey: ['playlists'] }), query.invalidateQueries({ queryKey: ['quota'] })])

  useEffect(() => { if (active && !player.playlistID) player.selectPlaylist(active.id) }, [active, player])
  useEffect(() => {
    if (!current) return
    engine.load(current.audioUrl)
    engine.setVolume(player.masterVolume * current.volumeRatio)
    const audio = engine.element()
    const start = current.playbackStartSeconds
    const end = current.playbackEndSeconds ?? current.durationSeconds
    const target = player.position >= start && player.position < end ? player.position : start
    engine.seek(target); setPosition(target)
    if (player.playing) engine.play().catch(() => player.setPlaying(false))
    else engine.pause()
  }, [current?.id, player.playing, player.masterVolume, current?.volumeRatio])

  const playTrack = (track: Track) => player.selectTrack(track.id, advanceRecovery(player.recovery, track.id))
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
  const [playlistName, setPlaylistName] = useState('')
  const library = useMemo(() => playlists.find((p) => p.isDefault)?.tracks ?? [], [playlists])

  if (isLoading) return <main className="center">正在载入音乐空间…</main>
  if (error) return <main className="center error">无法连接服务器：{error.message}</main>

  return <div className="shell">
    <aside>
      <header><span className="mark">♪</span><div><strong>音乐空间</strong><small>你的私人播放站</small></div></header>
      <nav>{playlists.map((p) => <button className={p.id === active?.id ? 'active' : ''} key={p.id} onClick={() => player.selectPlaylist(p.id)}><span>{p.isDefault ? '♫' : '≡'}</span>{p.name}<em>{p.tracks.length}</em></button>)}</nav>
      <form className="new-list" onSubmit={(e) => { e.preventDefault(); if (playlistName.trim()) { create.mutate(playlistName.trim()); setPlaylistName('') } }}><input value={playlistName} onChange={(e) => setPlaylistName(e.target.value)} placeholder="新建歌单"/><button>＋</button></form>
      <div className="quota"><span>存储空间</span><b>{bytes(quota?.usedBytes)} / {bytes(quota?.limitBytes)}</b><i><u style={{ width: `${Math.min(100, ((quota?.usedBytes ?? 0) / (quota?.limitBytes || 1)) * 100)}%` }}/></i></div>
    </aside>
    <main>
      <section className="hero"><div><small>当前歌单</small><h1>{active?.name ?? '默认歌单'}</h1><p>{active?.tracks.length ?? 0} 首音乐 · 加权随机播放</p></div><label className="upload"><input type="file" accept="audio/*,.aac,.m4a,.m4b,.mp4,.flac,.ogg,.opus,.wma,.wav" onChange={(e) => { const file=e.target.files?.[0]; if(file) upload.mutate(file); e.target.value='' }}/>{upload.isPending ? '处理中…' : '上传音乐'}</label></section>
      {upload.error && <p className="error">{upload.error.message}</p>}
      <section className="tracks">
        <div className="track heading"><span>#</span><span>歌曲</span><span>时长</span><span>权重</span><span>音量</span><span/></div>
        {active?.tracks.map((track, index) => <div className={`track ${current?.id === track.id ? 'playing' : ''}`} key={track.id}>
          <button className="play" onClick={() => current?.id === track.id && player.playing ? player.setPlaying(false) : playTrack(track)}>{current?.id === track.id && player.playing ? '❚❚' : '▶'}</button>
          <div><strong>{track.title}</strong><small>{track.artist || '未知艺术家'}{track.album ? ` · ${track.album}` : ''}</small></div>
          <span>{time(track.durationSeconds)}</span><span>{track.baseRandomWeight}</span><span>{Math.round(track.volumeRatio * 100)}%</span>
          <button className="edit" onClick={() => { lyricsSearch.reset(); setSelectedLyricsID(null); setEditing({ ...track }) }}>设置</button>
        </div>)}
        {!active?.tracks.length && <div className="empty"><b>这里还没有音乐</b><span>上传一个音频文件开始播放</span></div>}
      </section>
      {active && !active.isDefault && library.length > 0 && <details className="add"><summary>从音乐库添加歌曲</summary>{library.filter((t) => !active.tracks.some((x) => x.id === t.id)).map((t) => <button key={t.id} onClick={() => api.addTrack(active.id,t.id).then(refresh)}>＋ {t.title}</button>)}</details>}
      <LyricsView trackTitle={current?.title} lyrics={current?.lyrics ?? ''} position={position} onSeek={(seconds) => { engine.seek(seconds); setPosition(seconds); player.setPosition(seconds) }}/>
    </main>
    {editing && <div className="modal"><form onSubmit={(e) => { e.preventDefault(); save.mutate(editing) }}>
      <h2>歌曲设置</h2><p>{editing.title}</p>
      <label>随机权重<input type="number" min={Number.MIN_VALUE} step="any" value={editing.baseRandomWeight} onChange={(e) => setEditing({ ...editing, baseRandomWeight: Number(e.target.value) })}/></label>
      <div className="range"><label>开始（秒）<input type="number" min="0" step="0.1" value={editing.playbackStartSeconds} onChange={(e) => setEditing({ ...editing, playbackStartSeconds: Number(e.target.value) })}/></label><label>结束（秒）<input type="number" min="0" step="0.1" value={editing.playbackEndSeconds ?? ''} onChange={(e) => setEditing({ ...editing, playbackEndSeconds: e.target.value === '' ? null : Number(e.target.value) })}/></label></div>
      <label>单曲音量 {Math.round(editing.volumeRatio*100)}%<input type="range" min="0" max="1" step="0.01" value={editing.volumeRatio} onChange={(e) => setEditing({ ...editing, volumeRatio: Number(e.target.value) })}/></label>
      <div className="lyrics-editor">
        <div className="lyrics-tools"><span>歌词</span><button type="button" onClick={() => lyricsSearch.mutate(editing.id)} disabled={lyricsSearch.isPending}>{lyricsSearch.isPending ? '正在搜索…' : '从 LRCLIB 匹配'}</button></div>
        {lyricsSearch.error && <small className="error">{lyricsSearch.error.message}</small>}
        {lyricsSearch.data && <div className="lyrics-results">
          <small>使用“{lyricsSearch.data.queryTitle}”{lyricsSearch.data.queryArtist ? ` / “${lyricsSearch.data.queryArtist}”` : ''}找到 {lyricsSearch.data.candidates.length} 个候选</small>
          {lyricsSearch.data.candidates.map((candidate) => <button type="button" className={selectedLyricsID === candidate.id ? 'selected' : ''} key={candidate.id} onClick={() => { setSelectedLyricsID(candidate.id); setEditing({ ...editing, lyrics: candidate.lyrics }) }}>
            <strong>{candidate.trackName}</strong><span>{candidate.artistName || '未知艺术家'} · {time(candidate.duration)} · {candidate.synced ? '同步歌词' : '纯文本'}</span>
          </button>)}
          {!lyricsSearch.data.candidates.length && <span className="no-result">没有找到可用歌词</span>}
        </div>}
        <textarea aria-label="歌词" rows={8} value={editing.lyrics} onChange={(e) => { setSelectedLyricsID(null); setEditing({ ...editing, lyrics: e.target.value }) }}/>
      </div>
      <div className="actions"><button type="button" onClick={() => setEditing(null)}>取消</button><button className="primary">保存</button></div>
    </form></div>}
    <footer className="player"><div className="now"><span className="cover">♪</span><div><strong>{current?.title ?? '选择一首音乐'}</strong><small>{current?.artist || '尚未播放'}</small></div></div><div className="controls"><div><button onClick={() => player.setMode(player.mode === 'random' ? 'repeat-one' : 'random')}>{player.mode === 'random' ? '⤨' : '↻'}</button><button className="main" disabled={!current} onClick={() => player.setPlaying(!player.playing)}>{player.playing ? '❚❚' : '▶'}</button><button disabled={!active?.tracks.length} onClick={next}>▶|</button></div><label><span>{time(position)}</span><input type="range" min={current?.playbackStartSeconds ?? 0} max={current?.playbackEndSeconds ?? current?.durationSeconds ?? 1} step="0.1" value={position} onChange={(e) => { const p=Number(e.target.value);setPosition(p);engine.seek(p);player.setPosition(p) }}/><span>{time(current?.playbackEndSeconds ?? current?.durationSeconds)}</span></label></div><label className="volume">♬<input type="range" min="0" max="1" step="0.01" value={player.masterVolume} onChange={(e) => player.setMasterVolume(Number(e.target.value))}/></label></footer>
  </div>
}
