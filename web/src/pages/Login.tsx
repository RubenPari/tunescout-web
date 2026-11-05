import { useSearchParams } from 'react-router'
import { Band } from '../components/Dial'

export default function Login() {
  const [params] = useSearchParams()
  const error = params.get('error')
  return (
    <div className="mx-auto flex min-h-screen max-w-3xl flex-col justify-center px-6 py-16">
      <h1 className="display text-[clamp(3rem,11vw,6.5rem)]">tunescout</h1>
      <p className="mt-4 max-w-[52ch] text-lg text-dim">
        Musica nuova partendo dalla tua libreria Spotify. Claude propone i brani, tunescout li cerca uno per uno su
        Spotify e scarta quelli che conosci già.
      </p>

      <div className="mt-12">
        <Band />
      </div>

      {error && (
        <div role="alert" className="mt-8 rounded-lg border-l-[3px] border-alarm bg-alarm-soft px-4 py-3 text-[15px]">
          {error}
        </div>
      )}

      <div className="mt-10">
        <a href="/api/auth/login" className="btn-primary px-6 py-3 text-base">
          Accedi con Spotify
        </a>
      </div>

      <ul className="mt-14 grid gap-6 text-[15px] text-dim sm:grid-cols-3">
        <li>
          <strong className="block font-semibold text-fg">Scopri</strong>
          Suggerimenti su misura, anche per mood e generi.
        </li>
        <li>
          <strong className="block font-semibold text-fg">Verifica</strong>
          Ogni brano esiste davvero su Spotify: niente titoli inventati.
        </li>
        <li>
          <strong className="block font-semibold text-fg">Fai pulizia</strong>
          Trova i brani salvati che non ascolti più.
        </li>
      </ul>
    </div>
  )
}
