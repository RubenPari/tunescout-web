import { useState } from 'react'
import { api, ApiError, type PruneParams, type PruneResult } from '../api'
import ConfirmDialog from '../components/ConfirmDialog'
import ErrorBox from '../components/ErrorBox'
import JobLog from '../components/JobLog'
import PlaylistButton from '../components/PlaylistButton'
import TrackCard from '../components/TrackCard'
import { useJob } from '../useJob'

const dateFmt = new Intl.DateTimeFormat('it-IT', { dateStyle: 'medium' })

export default function Prune() {
  const job = useJob<PruneResult>('prune')
  const [form, setForm] = useState<PruneParams>({ count: 20, minAgeDays: 90 })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [removed, setRemoved] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<ApiError | null>(null)

  const result = job.job?.status === 'done' ? job.job.result : undefined
  const items = (result?.items ?? []).filter((i) => !removed.has(i.uri))

  const submit = () => {
    setSelected(new Set())
    setRemoved(new Set())
    setActionError(null)
    void job.start(form)
  }

  const toggle = (uri: string) =>
    setSelected((s) => {
      const n = new Set(s)
      if (n.has(uri)) n.delete(uri)
      else n.add(uri)
      return n
    })

  const remove = async () => {
    if (!job.job) return
    setBusy(true)
    setActionError(null)
    try {
      const uris = [...selected]
      await api.pruneRemove(job.job.id, uris)
      setRemoved((r) => new Set([...r, ...uris]))
      setSelected(new Set())
    } catch (e) {
      setActionError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  const allSelected = items.length > 0 && items.every((i) => selected.has(i.uri))

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-bold">Fai pulizia</h1>
        <p className="mt-1 text-zinc-400">
          Spotify non espone il numero di ascolti: un brano è considerato "poco ascoltato" se è salvato da almeno N
          giorni, non è tra i tuoi top brani e nessuno dei suoi artisti è tra i 50 che ascolti di più. Tra questi, Claude
          sceglie quelli meno in linea con il tuo profilo. Nulla viene rimosso senza conferma.
        </p>
      </header>

      <form
        className="card flex flex-wrap items-end gap-4 p-5"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <div className="w-32">
          <label className="label" htmlFor="pcount">
            Brani
          </label>
          <input
            id="pcount"
            className="input"
            type="number"
            min={1}
            max={50}
            value={form.count}
            onChange={(e) => setForm({ ...form, count: Math.max(1, Math.min(50, Number(e.target.value) || 1)) })}
          />
        </div>
        <div className="w-48">
          <label className="label" htmlFor="minage">
            Salvati da almeno (giorni)
          </label>
          <input
            id="minage"
            className="input"
            type="number"
            min={0}
            max={3650}
            value={form.minAgeDays}
            onChange={(e) => setForm({ ...form, minAgeDays: Math.max(0, Math.min(3650, Number(e.target.value) || 0)) })}
          />
        </div>
        <button className="btn-primary ml-auto" disabled={job.running}>
          {job.running ? 'Analizzo…' : 'Trova brani da eliminare'}
        </button>
      </form>

      <ErrorBox error={job.error} />
      <JobLog logs={job.logs} running={job.running} />

      {result && (
        <section className="space-y-4">
          {removed.size > 0 && (
            <div className="rounded-xl border border-emerald-900 bg-emerald-950/40 px-4 py-3 text-sm text-emerald-200">
              ✓ {removed.size} {removed.size === 1 ? 'brano rimosso' : 'brani rimossi'} dalla libreria. Non verranno più
              proposti da Scopri.
            </div>
          )}
          {items.length === 0 ? (
            removed.size === 0 && (
              <p className="text-zinc-400">
                Nessun brano da eliminare: la tua libreria è in linea con i tuoi gusti
                {result.stats.candidates === 0 && ' (nessun brano poco ascoltato con questi criteri)'}.
              </p>
            )
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <label className="mr-auto flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-emerald-500"
                    checked={allSelected}
                    onChange={() => setSelected(allSelected ? new Set() : new Set(items.map((i) => i.uri)))}
                  />
                  Seleziona tutti ({items.length})
                </label>
                <PlaylistButton
                  uris={selected.size ? [...selected] : items.map((i) => i.uri)}
                  defaultName="tunescout · da eliminare"
                  description="Brani da valutare per l'eliminazione, scelti da tunescout"
                  label={selected.size ? `Playlist di backup (${selected.size})` : 'Playlist di backup'}
                />
                <button className="btn-danger" disabled={selected.size === 0} onClick={() => setConfirm(true)}>
                  Rimuovi selezionati ({selected.size})
                </button>
              </div>
              <p className="text-xs text-zinc-500">
                {result.stats.saved} brani salvati · {result.stats.candidates} poco ascoltati
              </p>
              <ErrorBox error={actionError} />
              <ul className="space-y-3">
                {items.map((t) => (
                  <TrackCard
                    key={t.uri}
                    track={t}
                    note={`Salvato il ${dateFmt.format(new Date(t.addedAt))}`}
                    leading={
                      <input
                        type="checkbox"
                        aria-label={`Seleziona ${t.title}`}
                        className="mt-1 h-5 w-5 shrink-0 accent-rose-500"
                        checked={selected.has(t.uri)}
                        onChange={() => toggle(t.uri)}
                      />
                    }
                  />
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      <ConfirmDialog
        open={confirm}
        title={`Rimuovere ${selected.size} ${selected.size === 1 ? 'brano' : 'brani'} dalla libreria?`}
        confirmLabel={busy ? 'Rimuovo…' : 'Rimuovi'}
        danger
        busy={busy}
        onConfirm={remove}
        onCancel={() => setConfirm(false)}
      >
        I brani verranno tolti dai "Brani che ti piacciono" su Spotify. Se vuoi conservarne una copia, crea prima una
        playlist di backup.
      </ConfirmDialog>
    </div>
  )
}
