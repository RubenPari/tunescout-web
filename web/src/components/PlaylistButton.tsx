import { useState } from 'react'
import { api, toApiError, type Playlist } from '../api'

interface Props {
  uris: string[]
  defaultName: string
  description?: string
  label?: string
}

/** Crea una playlist privata con i brani indicati, chiedendo il nome. */
export default function PlaylistButton({ uris, defaultName, description, label = 'Crea playlist' }: Props) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState(defaultName)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<Playlist | null>(null)
  const [error, setError] = useState('')

  if (done) {
    return (
      <a className="btn-ghost" href={done.url} target="_blank" rel="noreferrer">
        Apri «{done.name}» in Spotify
      </a>
    )
  }
  if (!open) {
    return (
      <button className="btn-ghost" disabled={uris.length === 0} onClick={() => setOpen(true)}>
        {label}
      </button>
    )
  }
  const submit = async () => {
    setBusy(true)
    setError('')
    try {
      setDone(await api.createPlaylist(name, uris, description))
    } catch (e) {
      setError(toApiError(e).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <form
      className="flex flex-wrap items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <input
        className="input w-56"
        value={name}
        maxLength={100}
        onChange={(e) => setName(e.target.value)}
        aria-label="Nome della playlist"
        autoFocus
      />
      <button className="btn-primary" disabled={busy || !name.trim()}>
        {busy ? 'Creo…' : `Crea (${uris.length})`}
      </button>
      <button type="button" className="btn-ghost" onClick={() => setOpen(false)} disabled={busy}>
        Annulla
      </button>
      {error && <span className="text-sm text-alarm">{error}</span>}
    </form>
  )
}
