import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'
import { api } from './api'
import { initializePlayerSession } from './player-store'
import './styles.css'

const client = new QueryClient({ defaultOptions: { queries: { staleTime: 5_000, retry: 1 } } })
const root = ReactDOM.createRoot(document.getElementById('root')!)
root.render(<main className="center">正在载入音乐空间…</main>)
api.currentUser().then((user) => {
  if (typeof user.id !== 'string' || !user.id || typeof user.isDefault !== 'boolean') throw new Error('Invalid current user')
  initializePlayerSession(user)
  root.render(<React.StrictMode><QueryClientProvider client={client}><App /></QueryClientProvider></React.StrictMode>)
}).catch(() => {
  root.render(<main className="center error"><div>无法连接服务器，未恢复播放会话。<button onClick={() => window.location.reload()}>重试</button></div></main>)
})
