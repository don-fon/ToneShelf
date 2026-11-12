import { useEffect, useState } from 'react'

export function useNarrowLayout() {
  const [narrow, setNarrow] = useState(() => window.matchMedia?.('(max-width:800px)').matches ?? false)
  useEffect(() => {
    const media = window.matchMedia?.('(max-width:800px)')
    if (!media) return
    const update = () => setNarrow(media.matches)
    update(); media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  return narrow
}
