import { useState } from 'react'
import { Link } from 'react-router'
import { api, type Me, type SyncResult } from '../api'
import ConfirmDialog from '../components/ConfirmDialog'
import ErrorBox from '../components/ErrorBox'
import JobLog from '../components/JobLog'
import { useJob } from '../useJob'

const fmt = new Intl.DateTimeFormat('it-IT', { dateStyle: 'medium', timeStyle: 'short' })

export default function Dashboard({ me }: { me: Me }) {
  const sync = useJob<SyncResult>('sync')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const busy = sync.running || (me.activeJob?.status === 'running' && me.activeJob.kind !== 'sync')
  const { library, usage } = me

  const deleteAccount = async () => {
    setDeleting(true)
    try {
      await api.deleteAccount()
      sessionStorage.clear()
      window.location.href = '/'
    } catch {
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div className="space-y-8">
      <section>
        <h1 className="text-2xl font-bold">Ciao, {me.user.displayName}</h1>
        <p className="mt-1 text-zinc-400">
          {library.lastSync
            ? `Libreria sincronizzata il ${fmt.format(new Date(library.lastSync))}.`
            : 'Sincronizza la libreria per iniziare: tunescout legge i brani salvati, i tuoi top artisti e brani e le tue playlist.'}
        </p>
      </section>

      <section className="grid gap-4 sm:grid-cols-3">
        <Stat label="Brani noti" value={library.tracks.toLocaleString('it-IT')} />
        <Stat label="Artisti" value={library.artists.toLocaleString('it-IT')} />
        <Stat
          label="Ricerche oggi"
          value={usage.limit ? `${usage.today} / ${usage.limit}` : String(usage.today)}
          hint="discover e prune"
        />
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <button className="btn-primary" disabled={busy} onClick={() => sync.start()}>
            {sync.running ? 'Sincronizzo…' : library.lastSync ? 'Aggiorna libreria' : 'Sincronizza libreria'}
          </button>
          <span className="text-sm text-zinc-500">
            Avviene in automatico prima di Scopri e Fai pulizia se l'ultima sincronizzazione ha più di 24 ore.
          </span>
        </div>
        <ErrorBox error={sync.error} />
        <JobLog logs={sync.logs} running={sync.running} />
      </section>

      <section className="grid gap-4 sm:grid-cols-2">
        <Link to="/discover" className="card block p-6 transition hover:border-emerald-700">
          <h2 className="text-lg font-semibold">Scopri musica nuova →</h2>
          <p className="mt-1 text-sm text-zinc-400">
            Chiedi a Claude brani adatti a te, con mood e generi. Ogni proposta è verificata su Spotify.
          </p>
        </Link>
        <Link to="/prune" className="card block p-6 transition hover:border-emerald-700">
          <h2 className="text-lg font-semibold">Fai pulizia →</h2>
          <p className="mt-1 text-sm text-zinc-400">
            Trova i brani salvati che ascolti poco e che sono lontani dai tuoi gusti.
          </p>
        </Link>
      </section>

      <section className="border-t border-zinc-800 pt-6 text-sm text-zinc-500">
        <button className="text-rose-400 hover:text-rose-300" onClick={() => setConfirmDelete(true)}>
          Elimina il mio account
        </button>
      </section>

      <ConfirmDialog
        open={confirmDelete}
        title="Eliminare l'account?"
        confirmLabel={deleting ? 'Elimino…' : 'Elimina'}
        danger
        busy={deleting}
        onConfirm={deleteAccount}
        onCancel={() => setConfirmDelete(false)}
      >
        Verranno cancellati dal server la cache della libreria, il profilo, lo storico dei suggerimenti e il token di
        accesso a Spotify. La tua libreria Spotify non viene modificata.
      </ConfirmDialog>
    </div>
  )
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="card p-5">
      <div className="text-xs uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="mt-1 text-3xl font-bold">{value}</div>
      {hint && <div className="mt-1 text-xs text-zinc-500">{hint}</div>}
    </div>
  )
}
