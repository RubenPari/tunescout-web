import { useSearchParams } from 'react-router'
import Logo from '../components/Logo'

export default function Login() {
  const [params] = useSearchParams()
  const error = params.get('error')
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="w-full max-w-md text-center">
        <Logo className="mx-auto h-14 w-14" />
        <h1 className="mt-6 text-3xl font-bold tracking-tight">tunescout</h1>
        <p className="mt-3 text-zinc-400">
          Scopri musica nuova partendo dalla tua libreria Spotify. Claude propone i brani, tunescout li verifica uno per
          uno ed esclude quelli che conosci già.
        </p>
        {error && (
          <div role="alert" className="mt-6 rounded-xl border border-rose-900 bg-rose-950/50 px-4 py-3 text-sm text-rose-200">
            {error}
          </div>
        )}
        <a href="/api/auth/login" className="btn-primary mt-8 px-6 py-3 text-base">
          Accedi con Spotify
        </a>
        <ul className="mt-10 grid gap-3 text-left text-sm text-zinc-400 sm:grid-cols-3">
          <li className="card p-4">
            <span className="block font-semibold text-zinc-200">Scopri</span>
            Suggerimenti su misura, filtrabili per mood e generi.
          </li>
          <li className="card p-4">
            <span className="block font-semibold text-zinc-200">Verifica</span>
            Ogni brano viene cercato su Spotify: niente titoli inventati.
          </li>
          <li className="card p-4">
            <span className="block font-semibold text-zinc-200">Fai pulizia</span>
            Trova i brani salvati che non ascolti più.
          </li>
        </ul>
      </div>
    </div>
  )
}
