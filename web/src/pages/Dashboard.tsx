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

  const n = (v: number) => v.toLocaleString('it-IT')
  const usageText = usage.limit
    ? `Oggi hai usato ${usage.today} ricerche su ${usage.limit}.`
    : `Oggi hai fatto ${usage.today} ${usage.today === 1 ? 'ricerca' : 'ricerche'}.`

  return (
    <div className="space-y-12">
      <section>
        <p className="text-dim">Ciao, {me.user.displayName}</p>
        {library.lastSync ? (
          <>
            <h1 className="display mt-2 max-w-[22ch] text-[clamp(2rem,6vw,2.75rem)] tabular-nums">
              Nella tua libreria ci sono {n(library.tracks)} brani di {n(library.artists)} artisti.
            </h1>
            <p className="mt-3 text-dim">
              Sincronizzata il {fmt.format(new Date(library.lastSync))}. {usageText}
            </p>
          </>
        ) : (
          <>
            <h1 className="display mt-2 max-w-[22ch] text-[clamp(2rem,6vw,2.75rem)]">
              Sincronizza la libreria per iniziare.
            </h1>
            <p className="mt-3 max-w-[60ch] text-dim">
              tunescout legge i brani salvati, i tuoi artisti e brani più ascoltati e le tue playlist, così non ti
              propone musica che conosci già.
            </p>
          </>
        )}
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <button className="btn-primary" disabled={busy} onClick={() => sync.start()}>
            {sync.running ? 'Sincronizzo…' : library.lastSync ? 'Aggiorna libreria' : 'Sincronizza libreria'}
          </button>
          <span className="max-w-[56ch] text-sm text-dim">
            Scopri e Fai pulizia la aggiornano da soli se l'ultima sincronizzazione ha più di 24 ore.
          </span>
        </div>
        <ErrorBox error={sync.error} />
        <JobLog logs={sync.logs} running={sync.running} />
      </section>

      <section className="grid gap-x-10 gap-y-8 border-t border-line pt-8 sm:grid-cols-2">
        <Action
          to="/discover"
          title="Scopri musica nuova"
          text="Chiedi a Claude brani adatti a te, anche per mood e generi. Ogni proposta viene cercata su Spotify."
          cta="Trova brani"
        />
        <Action
          to="/prune"
          title="Fai pulizia"
          text="Trova i brani salvati che ascolti poco e che sono lontani dai tuoi gusti."
          cta="Analizza la libreria"
        />
      </section>

      <section className="border-t border-line pt-6 text-sm">
        <button className="text-alarm hover:underline" onClick={() => setConfirmDelete(true)}>
          Elimina il mio account
        </button>
      </section>

      <ConfirmDialog
        open={confirmDelete}
        title="Eliminare l'account?"
        confirmLabel={deleting ? 'Elimino…' : 'Elimina account'}
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

function Action({ to, title, text, cta }: { to: string; title: string; text: string; cta: string }) {
  return (
    <div>
      <h2 className="display text-2xl">{title}</h2>
      <p className="mt-2 max-w-[44ch] text-[15px] text-dim">{text}</p>
      <Link to={to} className="btn-ghost mt-4">
        {cta}
      </Link>
    </div>
  )
}
