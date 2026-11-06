import { useQueryClient } from '@tanstack/react-query'
import { NavLink, Outlet } from 'react-router'
import { api, type Me } from '../api'
import { leaveApp } from '../util'
import Dial, { type Station } from './Dial'
import Logo from './Logo'

const stations: Station[] = [
  { to: '/', label: 'Libreria', end: true },
  { to: '/discover', label: 'Scopri' },
  { to: '/prune', label: 'Fai pulizia' },
  { to: '/history', label: 'Storico' },
]

export default function Layout({ me }: { me: Me }) {
  const qc = useQueryClient()
  const logout = async () => {
    await api.logout()
    qc.clear()
    leaveApp()
  }
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-10 bg-night/90 backdrop-blur">
        <div className="mx-auto max-w-4xl px-4 pt-4">
          <div className="flex items-center gap-3">
            <NavLink to="/" className="flex items-center gap-2.5 rounded-md">
              <Logo />
              <span className="display text-xl">tunescout</span>
            </NavLink>
            <div className="ml-auto flex items-center gap-4 text-sm">
              <span className="hidden text-dim sm:inline">{me.user.displayName}</span>
              <button className="text-dim transition-colors hover:text-fg" onClick={logout}>
                Esci
              </button>
            </div>
          </div>
          <div className="mt-3">
            <Dial stations={stations} scanning={me.activeJob?.status === 'running'} />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-4xl px-4 pb-16 pt-10">
        <Outlet />
      </main>
    </div>
  )
}
