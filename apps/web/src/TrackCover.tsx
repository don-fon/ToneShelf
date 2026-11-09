import { useState } from 'react'

export function TrackCover({ src, className = 'cover', background = false }: { src?: string; className?: string; background?: boolean }) {
  const [failed, setFailed] = useState<string>()
  if (background) return src && failed !== src ? <img className={className} src={src} alt="" aria-hidden="true" onError={() => setFailed(src)}/> : null
  return <span className={className} aria-hidden="true">{src && failed !== src ? <img src={src} alt="" loading="lazy" onError={() => setFailed(src)}/> : '♫'}</span>
}
