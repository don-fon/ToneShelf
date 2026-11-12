import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { CurrentUser } from '@music/domain'
import { api, APIError } from './api'
import { App } from './App'
import { initializePlayerSession } from './player-store'
import { advanceAuthGeneration, watchAuthExpired } from './platform/auth-events'

export function AuthGate() {
  const query = useQueryClient()
  const [user, setUser] = useState<CurrentUser | null>(null)
  const [checking, setChecking] = useState(true)
  const [account, setAccount] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)
  const stopPlayerSession = useRef<(() => void) | undefined>(undefined)
  const accept = useCallback((value: CurrentUser) => {
    if (!value || typeof value.id !== 'string' || !value.id || typeof value.isDefault !== 'boolean') throw new Error('无法确认账号身份')
    advanceAuthGeneration()
    stopPlayerSession.current?.()
    stopPlayerSession.current = initializePlayerSession(value)
    setUser(value)
  }, [])
  const clear = useCallback(() => {
    generation.current++
    advanceAuthGeneration()
    stopPlayerSession.current?.(); stopPlayerSession.current = undefined
    setUser(null); setAccount(''); setError(''); setChecking(false)
    void query.cancelQueries(); query.clear()
  }, [query])
  useEffect(() => watchAuthExpired(clear), [clear])
  useEffect(() => {
    const version = ++generation.current
    let active = true
    api.currentUser().then((value) => { if (active && generation.current === version) accept(value) }).catch((reason) => {
      if (active && !(reason instanceof APIError && reason.status === 401)) setError('无法连接服务器，请重试。')
    }).finally(() => { if (active) setChecking(false) })
    return () => { active = false; generation.current++; stopPlayerSession.current?.(); stopPlayerSession.current = undefined }
  }, [accept])
  const login = async () => {
    if (busy || !account) return
    setBusy(true); setError('')
    try { const value = await api.login(account); setAccount(''); accept(value) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '登录失败，请重试') }
    finally { setBusy(false) }
  }
  const logout = async () => {
    if (busy || !window.confirm('确定退出登录？')) return
    setBusy(true); setError('')
    try { await api.logout(); clear() }
    catch (reason) { setError(reason instanceof Error ? reason.message : '退出失败，请重试') }
    finally { setBusy(false) }
  }
  if (checking) return <main className="center">正在连接…</main>
  if (!user) return <main className="login-screen"><form onSubmit={(event) => { event.preventDefault(); void login() }}><h1>登录音乐库</h1><label>账号<input name="account" type="text" autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={256} value={account} onChange={(event) => setAccount(event.target.value)} disabled={busy} required/></label><small>使用管理员提供的账号登录</small>{error && <p role="alert">{error}</p>}<button className="primary" disabled={busy || !account}>{busy ? '正在登录…' : '登录'}</button></form></main>
  return <><App key={user.id} onLogout={() => void logout()} logoutBusy={busy}/>{error && <div className="auth-error" role="alert">{error}</div>}</>
}
