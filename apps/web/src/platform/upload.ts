import type { UploadJob } from '@music/domain'

export function uploadFile(url: string, file: File, onProgress?: (percent: number) => void, requestKey?: string): Promise<UploadJob> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url)
    if (requestKey) xhr.setRequestHeader('X-Upload-Key', requestKey)
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(Math.min(100, Math.round(event.loaded / event.total * 100)))
    }
    xhr.onerror = () => reject(new Error('上传连接中断，请检查任务列表后重新上传'))
    xhr.onabort = () => reject(new Error('上传已中断'))
    xhr.onload = () => {
      try {
        const body = JSON.parse(xhr.responseText)
        if (xhr.status >= 200 && xhr.status < 300) resolve(body as UploadJob)
        else reject(new Error(body.error || '上传失败'))
      } catch { reject(new Error('无法读取上传响应，请检查任务列表')) }
    }
    const data = new FormData()
    data.append('file', file)
    xhr.send(data)
  })
}
