import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { Tag, TagGroup } from '@music/domain'
import { allTagIDs, tagGroupNameError, tagNameError, UNTAGGED_ID } from '@music/player-policy'
import { api } from './api'

export function TagChoices({ tags, enabled, onChange, untagged = false, onExclusive, children, disabled = false }: { tags: Tag[]; enabled: string[]; onChange: (ids: string[]) => void; untagged?: boolean; onExclusive?: (id: string) => void; children?: ReactNode; disabled?: boolean }) {
  const lastTouch = useRef<{ id: string; time: number } | null>(null)
  const suppressedClick = useRef<{ id: string; time: number } | null>(null)
  const choices = untagged ? [{ id: UNTAGGED_ID, name: '无' }, ...tags] : tags
  return <div className="tag-choices">{choices.map((tag) => <button type="button" className="tag-chip" disabled={disabled} key={tag.id} title={(tag.id === UNTAGGED_ID ? '没有任何标签的歌曲' : tag.name) + (onExclusive ? '（双击只开启此标签）' : '')} aria-label={`标签 ${tag.name}`} aria-pressed={enabled.includes(tag.id)}
    onPointerDown={onExclusive ? () => { suppressedClick.current = null } : undefined}
    onPointerCancel={onExclusive ? () => { lastTouch.current = null; suppressedClick.current = null } : undefined}
    onPointerUp={onExclusive ? (event) => {
      if (event.pointerType !== 'touch') return
      const previous = lastTouch.current
      if (previous?.id === tag.id && event.timeStamp - previous.time <= 350) {
        lastTouch.current = null; suppressedClick.current = { id: tag.id, time: event.timeStamp }; onExclusive(tag.id)
      } else lastTouch.current = { id: tag.id, time: event.timeStamp }
    } : undefined}
    onDoubleClick={onExclusive ? (event) => { event.preventDefault(); lastTouch.current = null; onExclusive(tag.id) } : undefined}
    onClick={(event) => {
      const suppressed = suppressedClick.current; suppressedClick.current = null
      if (suppressed?.id === tag.id && event.timeStamp - suppressed.time <= 500) return
      onChange(enabled.includes(tag.id) ? enabled.filter((id) => id !== tag.id) : [...enabled, tag.id])
    }}>{tag.name}</button>)}{children}</div>
}

export function TrackTagBadges({ tags, ids = [] }: { tags: Tag[]; ids?: string[] }) {
  const names = tags.filter((tag) => ids.includes(tag.id)).map((tag) => tag.name)
  if (!names.length) return null
  return <span className="track-tags" aria-label={`歌曲标签：${names.join('、')}`}>{names.map((name) => <span key={name} title={name}>{name}</span>)}</span>
}

type GroupDraft = { id?: string; name: string; tagIds: string[] }

export function TagFilter({ tags, enabled, onChange, children, editing = false, onSaved, groupEditing = false, disabled = false }: { tags: Tag[]; enabled: string[]; onChange: (ids: string[] | undefined) => void; children?: ReactNode; editing?: boolean; onSaved?: () => Promise<unknown>; groupEditing?: boolean; disabled?: boolean }) {
  return <section className="tag-filter" role={groupEditing ? 'group' : undefined} aria-label={groupEditing ? '标签组开关' : '标签筛选'} data-editing={editing} data-group-editing={groupEditing}>{editing && onSaved ? <TagManager tags={tags} onSaved={onSaved}/> : <TagChoices tags={tags} enabled={enabled} onChange={onChange} onExclusive={groupEditing ? undefined : (id) => onChange([id])} disabled={disabled} untagged/>}{children}</section>
}

export function TagPanel({ groups, tags, enabled, onChange, onSaved, children, selecting = false, selectionAnchor }: { groups: TagGroup[]; tags: Tag[]; enabled: string[]; onChange: (ids: string[] | undefined) => void; onSaved: () => Promise<unknown>; children?: ReactNode; selecting?: boolean; selectionAnchor?: (node: HTMLDivElement | null) => void }) {
  const [open, setOpen] = useState(false)
  const [editingTags, setEditingTags] = useState(false)
  const [groupDraft, setGroupDraft] = useState<GroupDraft | null>(null)
  const [groupBusy, setGroupBusy] = useState(false)
  const root = useRef<HTMLElement>(null)
  const toggle = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => { if (!document.querySelector('.modal,dialog[open]') && !root.current?.contains(event.target as Node)) setOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape' && !document.querySelector('.modal,dialog[open]')) { event.preventDefault(); setOpen(false); toggle.current?.focus() } }
    document.addEventListener('pointerdown', outside); document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [open])
  return <section ref={root} className="tag-panel" aria-label="标签与标签组" data-selecting={selecting}>
    <TagFilter tags={tags} enabled={groupDraft?.id ? groupDraft.tagIds : enabled} editing={editingTags && !groupDraft?.id} groupEditing={!!groupDraft?.id} disabled={groupBusy} onSaved={onSaved} onChange={(ids) => { if (groupDraft?.id) setGroupDraft({ ...groupDraft, tagIds: ids ?? [] }); else { onChange(ids); setOpen(false) } }}><span className="tag-group-controls"><button type="button" aria-label={editingTags ? '完成编辑' : '编辑标签和标签组'} title={editingTags ? '退出编辑' : '编辑标签和标签组'} aria-pressed={editingTags} disabled={groupBusy} onClick={() => { setEditingTags(!editingTags); if (editingTags) setGroupDraft(null) }}><svg viewBox="0 0 24 24" aria-hidden="true"><path d={editingTags ? 'm6 6 12 12M18 6 6 18' : 'm15 5 4 4M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z'}/></svg></button></span></TagFilter>
    <div className="tag-panel-group-area"><TagGroupNav draft={groupDraft} onDraftChange={setGroupDraft} onBusyChange={setGroupBusy} managing={editingTags} groups={groups} tags={tags} enabled={enabled} onApply={(ids) => { onChange(ids); setOpen(false) }} onSaved={onSaved}/></div>
    <div className="mobile-selection-slot" ref={selectionAnchor}/>
    <button ref={toggle} type="button" className="mobile-tools-toggle" aria-label="音乐库菜单" title="更多操作" aria-expanded={open} aria-controls="library-management-menu" onClick={() => setOpen(!open)}><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="5" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="12" cy="19" r="1.5"/></svg></button>
    <div id="library-management-menu" className="tag-panel-secondary" data-open={open} role="region" aria-label="音乐库管理">
      <button className="mobile-menu-upload" type="button" onClick={() => { document.getElementById('music-upload')?.click(); setOpen(false) }}>上传音乐</button>
      <div className="tag-panel-library-tools" onClick={(event) => { if ((event.target as Element).closest('.sidebar-tool-button')) setOpen(false) }}>{children}</div>
    </div>
  </section>
}

function InlineNameEditor({ value, onChange, onSave, onCancel, onDelete, busy, invalid, inputLabel, saveLabel, formLabel, deleteLabel }: { value: string; onChange: (value: string) => void; onSave: () => void; onCancel: () => void; onDelete?: () => void; busy: boolean; invalid: boolean; inputLabel: string; saveLabel: string; formLabel: string; deleteLabel?: string }) {
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { input.current?.focus({ preventScroll: true }); input.current?.select() }, [])
  return <form className="tag-inline-editor" aria-label={formLabel} onSubmit={(event) => { event.preventDefault(); if (!busy && !invalid && value.trim()) onSave() }} onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!busy) onCancel() } else if (event.key === 'Enter' && event.nativeEvent.isComposing) event.preventDefault() }}>
    <input ref={input} aria-label={inputLabel} value={value} disabled={busy} placeholder="名称" onChange={(event) => onChange(event.target.value)}/>
    <button type="submit" aria-label={saveLabel} title="保存（Enter）" disabled={busy || !value.trim() || invalid}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4L19 6"/></svg></button>
    <button type="button" aria-label="取消编辑" title="取消（Esc）" disabled={busy} onClick={onCancel}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg></button>
    {onDelete && <button className="tag-inline-delete" type="button" aria-label={deleteLabel} title="删除" disabled={busy} onClick={onDelete}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7"/></svg></button>}
  </form>
}

function TagManager({ tags, onSaved }: { tags: Tag[]; onSaved: () => Promise<unknown> }) {
  const [editing, setEditing] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const invalid = name.trim() ? tagNameError(name.trim()) : null
  const cancel = () => { if (!busy) { setEditing(null); setName(''); setError('') } }
  const start = (id: string, value: string) => { setEditing(id); setName(value); setError('') }
  const perform = async (action: () => Promise<unknown>) => {
    if (busy) return
    setBusy(true); setError('')
    try { await action(); await onSaved(); setEditing(null); setName('') }
    catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') }
    finally { setBusy(false) }
  }
  const editor = editing && <InlineNameEditor key={editing} value={name} onChange={setName} busy={busy} invalid={!!invalid} inputLabel="新标签名称" saveLabel="保存名称" formLabel="重命名标签" onCancel={cancel} onSave={() => void perform(() => api.saveTag(name.trim(), editing))} deleteLabel={`删除标签 ${tags.find(tag => tag.id === editing)?.name ?? ''}`} onDelete={editing ? () => { if (window.confirm(`删除标签“${tags.find(tag => tag.id === editing)?.name}”？歌曲会保留，其标签关联及标签组中的引用会移除。`)) void perform(() => api.deleteTag(editing)) } : undefined}/>
  return <div className="tag-edit-choices" role="group" aria-label="管理标签">
    <span className="tag-chip tag-system-chip" title="系统标签不能编辑">无</span>
    {tags.map(tag => editing === tag.id ? <div className="tag-editor-slot" key={tag.id}>{editor}</div> : <button key={tag.id} type="button" className="tag-chip tag-editable" aria-label={`重命名标签 ${tag.name}`} title="点击修改名称" disabled={busy} onClick={() => start(tag.id, tag.name)}>{tag.name}</button>)}
    {(invalid || error) && <small className="tag-edit-error" role="alert">{error || invalid}</small>}
  </div>
}

export function TagGroupNav({ groups, tags, enabled, onApply, onSaved, managing = false, draft: controlledDraft, onDraftChange, onBusyChange }: { draft?: GroupDraft | null; onDraftChange?: (draft: GroupDraft | null) => void; onBusyChange?: (busy: boolean) => void; managing?: boolean; groups: TagGroup[]; tags: Tag[]; enabled: string[]; onApply: (ids: string[] | undefined) => void; onSaved: () => Promise<unknown> }) {
  const [localDraft, setLocalDraft] = useState<GroupDraft | null>(null)
  const draft = controlledDraft === undefined ? localDraft : controlledDraft
  const setDraft = onDraftChange ?? setLocalDraft
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { if (!managing) { setDraft(null); setError('') } }, [managing])
  const same = (ids: string[]) => ids.length === enabled.length && ids.every(id => enabled.includes(id))
  const hasMatch = groups.some(group => same(group.isDefault ? allTagIDs(tags) : group.tagIds))
  const canAdd = managing || !hasMatch
  const invalid = draft?.name.trim() ? tagGroupNameError(draft.name) : null
  const cancel = () => { if (!busy) { setDraft(null); setError('') } }
  const perform = async (action: () => Promise<unknown>) => {
    if (busy) return
    setBusy(true); onBusyChange?.(true); setError('')
    try { await action(); await onSaved(); setDraft(null) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') }
    finally { setBusy(false); onBusyChange?.(false) }
  }
  const editor = draft && <InlineNameEditor key={draft.id ?? 'new'} value={draft.name} onChange={name => setDraft({ ...draft, name })} busy={busy} invalid={!!invalid} inputLabel="标签组名称" saveLabel="保存标签组" formLabel={draft.id ? '重命名标签组' : '保存当前为标签组'} onCancel={cancel} onSave={() => void perform(() => api.saveTagGroup(draft.name.trim(), draft.tagIds, draft.id))} deleteLabel="删除标签组" onDelete={draft.id ? () => { if (window.confirm('删除此标签组？标签和歌曲会保留。')) void perform(() => api.deleteTagGroup(draft.id!)) } : undefined}/>
  return <section className="tag-group-picker tag-group-inline-panel" aria-label="标签组管理">
    <nav className="tag-group-nav" aria-label="标签组">{groups.map(group => <div className="tag-group-entry" data-active={draft?.id ? draft.id === group.id : same(group.isDefault ? allTagIDs(tags) : group.tagIds)} data-editing={managing} key={group.id}>
      {draft?.id === group.id ? editor : <button type="button" className="tag-chip" title={group.isDefault && managing ? '系统标签组不能编辑' : group.name} disabled={busy || (managing && group.isDefault)} aria-pressed={draft?.id ? draft.id === group.id : same(group.isDefault ? allTagIDs(tags) : group.tagIds)} aria-label={managing ? group.isDefault ? '系统标签组 全部' : `重命名标签组 ${group.name}` : `应用标签组 ${group.name}`} onClick={() => { if (managing) { setDraft({ id: group.id, name: group.name, tagIds: [...group.tagIds] }); setError('') } else onApply(group.isDefault ? undefined : group.tagIds) }}>{group.isDefault ? '全部' : [...group.name].length > 4 ? [...group.name].slice(0, 3).join('') + '…' : group.name}</button>}
    </div>)}{(canAdd || (draft && !draft.id)) && <div className="tag-group-entry" data-active={!hasMatch}>{draft && !draft.id ? editor : <button className="tag-chip tag-group-add" type="button" aria-label="保存当前为标签组" title="保存当前为标签组" disabled={busy} onClick={() => { setDraft({ name: '', tagIds: [...enabled] }); setError('') }}>＋</button>}</div>}</nav>
    {(invalid || error) && <small className="tag-edit-error" role="alert">{error || invalid}</small>}
  </section>
}

export function BulkTagDialog({ tags, count, onSave, onCreateTag, onClose }: { tags: Tag[]; count: number; onSave: (ids: string[], mode: 'add' | 'remove') => Promise<unknown>; onCreateTag: (name: string) => Promise<Tag>; onClose: () => void }) {
  const [selected, setSelected] = useState<string[]>([])
  const [mode, setMode] = useState<'add' | 'remove'>('add')
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const invalid = name ? tagNameError(name.trim()) : null
  const create = async () => {
    if (busy || creating || !name.trim() || invalid) return
    setCreating(true); setError('')
    try { const tag = await onCreateTag(name.trim()); setSelected((ids) => ids.includes(tag.id) ? ids : [...ids, tag.id]); setName('') }
    catch (reason) { setError(reason instanceof Error ? reason.message : '创建失败') }
    finally { setCreating(false) }
  }
  const disabled = busy || creating
  return <div className="modal"><form className="tag-dialog" role="dialog" aria-modal="true" aria-label="设置歌曲标签" onSubmit={async (event) => { event.preventDefault(); if (disabled || !selected.length) return; setBusy(true); setError(''); try { await onSave(selected, mode); onClose() } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') } finally { setBusy(false) } }}>
    <h2>设置标签</h2><p>已选择 {count} 首歌曲</p>
    <label>操作<select value={mode} disabled={disabled} onChange={(event) => { setMode(event.target.value as typeof mode); setError('') }}><option value="add">添加标签</option><option value="remove">移除标签</option></select></label>
    <fieldset disabled={disabled}><legend>标签</legend><TagChoices tags={tags} enabled={selected} onChange={setSelected}/>{!tags.length && <p>{mode === 'add' ? '还没有标签，可在下方创建。' : '没有可移除的标签。'}</p>}</fieldset>
    {mode === 'add' && <div className="inline-tag-create"><label>新建标签<input  disabled={disabled} value={name} placeholder="最多两个全角 / 四个半角" onChange={(event) => setName(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); void create() } }}/></label><button type="button" disabled={disabled || !name.trim() || !!invalid} onClick={() => void create()}>{creating ? '创建中…' : '创建并添加'}</button>{invalid && <small className="error">{invalid}</small>}</div>}
    {error && <p role="alert" className="error">{error}</p>}
    <div className="actions"><button type="button" disabled={disabled} onClick={onClose}>取消</button><button className="primary" disabled={disabled || !selected.length}>保存</button></div>
  </form></div>
}
