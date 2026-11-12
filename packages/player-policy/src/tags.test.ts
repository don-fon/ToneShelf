import { expect, it } from 'vitest'
import type { Track } from '@music/domain'
import { filterTracks, tagNameError, tagGroupNameError, UNTAGGED_ID } from './tags'
import { readPlayback } from './session'

it('accepts arbitrary characters within four halfwidth units and reserves the system label', () => {
  for (const name of ['日语','Rock','カナ','日AB','1234','a b','日-','🎵AB','👩🏽‍💻AB','🇯🇵AB','éABC','ｶﾅAB','<>&"']) expect(tagNameError(name)).toBeNull()
  for (const name of ['五个字标签','中文歌','日ABC','ABCDE','无',' 无 ','','   ','🎵🎵🎵','́']) expect(tagNameError(name)).not.toBeNull()
})
it('counts group names as up to four Unicode characters, independently from tag width rules', () => {
  for (const name of ['日语歌曲','Rock','🎵🎵🎵🎵',' 日语 ']) expect(tagGroupNameError(name)).toBeNull()
  for (const name of ['', '   ', '五个字的组','ABCDE','🎵🎵🎵🎵🎵']) expect(tagGroupNameError(name)).not.toBeNull()
})
it('matches any enabled tag, includes untagged only through its own switch and never duplicates a song', () => {
  const tracks = [{ id:'one',tagIds:['jp','rock'] },{id:'two',tagIds:['rock']},{id:'three',tagIds:[]}] as Track[]
  expect(filterTracks(tracks,['jp','rock']).map((t) => t.id)).toEqual(['one','two'])
  expect(filterTracks(tracks,['jp',UNTAGGED_ID]).map((t) => t.id)).toEqual(['one','three'])
  expect(filterTracks(tracks,[])).toEqual([])
})
it('restores explicit all-off switches and keeps the default all-tags mode distinct', () => {
  expect(readPlayback({ tagSelection: [] }).tagSelection).toEqual([])
  expect(readPlayback({}).tagSelection).toBeUndefined()
  expect(readPlayback({ tagSelection: ['jp','jp',null,3] }).tagSelection).toEqual(['jp'])
})
