import { useQuery } from '@tanstack/react-query'
import { Navigate, Route, Routes } from 'react-router'
import { api, ApiError, queryKeys } from './api'
import Layout from './components/Layout'
import Spinner from './components/Spinner'
import Dashboard from './pages/Dashboard'
import Discover from './pages/Discover'
import History from './pages/History'
import Login from './pages/Login'
import Prune from './pages/Prune'

export default function App() {
  const me = useQuery({ queryKey: queryKeys.me, queryFn: api.me })

  if (me.isPending) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  const loggedOut = me.error instanceof ApiError && me.error.status === 401
  if (loggedOut) {
    return (
      <Routes>
        <Route path="*" element={<Login />} />
      </Routes>
    )
  }
  if (me.isError) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-6 text-center">
        <p className="max-w-[48ch] text-fg">Il server non risponde ({me.error.message}). Controlla la connessione e riprova.</p>
        <button className="btn-ghost" onClick={() => me.refetch()}>
          Riprova
        </button>
      </div>
    )
  }
  return (
    <Routes>
      <Route element={<Layout me={me.data} />}>
        <Route index element={<Dashboard me={me.data} />} />
        <Route path="discover" element={<Discover />} />
        <Route path="prune" element={<Prune />} />
        <Route path="history" element={<History />} />
        <Route path="login" element={<Navigate to="/" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
