interface DocumentPictureInPicture {
  requestWindow: (options: { width: number; height: number }) => Promise<Window>
}

export async function openLyricsWindow(): Promise<Window> {
  if (!window.isSecureContext) throw new Error('悬浮歌词需要 HTTPS 或本机 localhost 环境。')
  const api = (window as Window & { documentPictureInPicture?: DocumentPictureInPicture }).documentPictureInPicture
  if (!api) throw new Error('当前浏览器不支持悬浮歌词，请使用支持文档画中画的桌面浏览器。')
  return api.requestWindow({ width: 480, height: 260 })
}
