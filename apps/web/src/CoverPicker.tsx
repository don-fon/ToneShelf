import { useEffect, useRef, useState } from 'react'
import type { Track } from '@music/domain'
import { TrackCover } from './TrackCover'

const maxCoverBytes = 8 * 1024 * 1024
const imageAccept = 'image/jpeg,image/png,image/webp,image/gif,image/bmp,.jpg,.jpeg,.png,.webp,.gif,.bmp'

interface Props {
  track: Track
  file: File | null
  onChange: (file: File | null) => void
  onInvalidChange: (invalid: boolean) => void
}

export function CoverPicker({ track, file, onChange, onInvalidChange }: Props) {
  const input = useRef<HTMLInputElement>(null)
  const [preview, setPreview] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    if (!file) { setPreview(''); return }
    const url = URL.createObjectURL(file)
    setPreview(url)
    return () => URL.revokeObjectURL(url)
  }, [file])
  const clear = () => { onChange(null); onInvalidChange(false); setError('') }

  return <div className="cover-editor"><button className="settings-file-button" type="button" aria-label="替换封面" title="JPG / PNG / WebP / GIF / BMP，最大 8 MiB" onClick={() => input.current?.click()}>{file && preview ? <span className="settings-cover"><img key={preview} src={preview} alt="新封面预览" onError={() => { onInvalidChange(true); setError('无法预览这张图片，请选择其他图片') }}/></span> : <TrackCover src={track.coverUrl} className="settings-cover"/>}<span>替换封面</span></button>
    <input ref={input} hidden type="file" aria-label="选择封面图片" accept={imageAccept} onChange={(event) => {
      const selected = event.target.files?.[0]
      event.target.value = ''
      if (!selected) return
      clear()
      if (!selected.size || selected.size > maxCoverBytes) { setError('请选择不超过 8 MiB 的非空图片'); return }
      onChange(selected)
    }}/>
    {error && <p className="error" role="alert">{error}</p>}
  </div>
}
