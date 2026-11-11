import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthGate } from './AuthGate'
import './styles.css'

const client = new QueryClient({ defaultOptions: { queries: { staleTime: 5_000, retry: 1 } } })
const root = ReactDOM.createRoot(document.getElementById('root')!)
root.render(<React.StrictMode><QueryClientProvider client={client}><AuthGate /></QueryClientProvider></React.StrictMode>)
