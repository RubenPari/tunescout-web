import { useQueryClient } from '@tanstack/react-query'
import { NavLink, Outlet } from 'react-router'
import { api, type Me } from '../api'
import Logo from './Logo'

const links = [
  { to: '/', label: 'Libreria', end: true },
  { to: '/discover', label: 'Scopri' },
  { to: '/prune', label: 'Fai pulizia' },
  { to: '/history', label: 'Storico' },
]

export default function Layout({ me }: { me: Me }) {
  const qc = useQueryClient()
  const logout = async () => {
    await api.logout()
    sessionStorage.clear()
    qc.clear()
    window.location.href = '/'
  }
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-950/85 backdrop-blur">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <NavLink to="/" className="flex items-center gap-2 font-bold">
            <Logo />
            tunescout
          </NavLink>
          <nav className="order-3 flex w-full gap-1 overflow-x-auto sm:order-none sm:w-auto">
            {links.map((l) => (
              <NavLink
                key={l.to}
                to={l.to}
                end={l.end}
                className={({ isActive }) =>
                  `whitespace-nowrap rounded-full px-3 py-1.5 text-sm transition ${
                    isActive ? 'bg-zinc-800 text-white' : 'text-zinc-400 hover:text-white'
                  }`
                }
              >
                {l.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <span className="hidden text-zinc-400 sm:inline">{me.user.displayName}</span>
            <button className="text-zinc-400 hover:text-white" onClick={logout}>
              Esci
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-8">
        <Outlet />
      </main>
    </div>
  )
}
