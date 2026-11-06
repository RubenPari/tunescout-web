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
  const [kept, setKept] = useState<Set<string>>(new Set())
  const [keeping, setKeeping] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<ApiError | null>(null)

  const result = job.job?.status === 'done' ? job.job.result : undefined
  const items = (result?.items ?? []).filter((i) => !removed.has(i.uri))
  const removable = items.filter((i) => !kept.has(i.uri))

  const submit = () => {
    setSelected(new Set())
    setRemoved(new Set())
    setKept(new Set())
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

  const toggleKeep = async (uri: string) => {
    if (!job.job || keeping.has(uri)) return
    const keep = !kept.has(uri)
    setKeeping((k) => new Set(k).add(uri))
    setActionError(null)
    try {
      await api.pruneKeep(job.job.id, uri, keep)
      setKept((k) => {
        const n = new Set(k)
        if (keep) n.add(uri)
        else n.delete(uri)
        return n
      })
      if (keep)
        setSelected((s) => {
          const n = new Set(s)
          n.delete(uri)
          return n
        })
    } catch (e) {
      setActionError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    } finally {
      setKeeping((k) => {
        const n = new Set(k)
        n.delete(uri)
        return n
      })
    }
  }

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

  const allSelected = removable.length > 0 && removable.every((i) => selected.has(i.uri))

  return (
    <div className="space-y-8">
      <header>
        <h1 className="display text-[clamp(2.25rem,7vw,3.25rem)]">Fai pulizia</h1>
        <p className="mt-2 max-w-[62ch] text-dim">
          Spotify non dice quante volte hai ascoltato un brano. Per questo un brano conta come poco ascoltato se è
          salvato da un certo numero di giorni, non è tra i tuoi brani più ascoltati e nessuno dei suoi artisti è tra i
          50 che ascolti di più. Tra questi, Claude sceglie quelli più lontani dai tuoi gusti.
        </p>
        <p className="mt-2 text-dim">
          Nulla viene rimosso senza la tua conferma. I brani che segni come da tenere non ti verranno più proposti.
        </p>
      </header>

      <form
        className="panel flex flex-wrap items-end gap-5 p-5 sm:p-6"
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
        <div className="w-52">
          <label className="label" htmlFor="minage">
            Salvati da almeno, in giorni
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
            <div role="status" className="rounded-lg border-l-[3px] border-dial bg-dial-soft px-4 py-3 text-[15px]">
              {removed.size === 1 ? 'Rimosso 1 brano' : `Rimossi ${removed.size} brani`} dalla libreria. Scopri non li
              proporrà più.
            </div>
          )}
          {kept.size > 0 && (
            <div role="status" className="rounded-lg border-l-[3px] border-dial bg-dial-soft px-4 py-3 text-[15px]">
              {kept.size === 1 ? '1 brano tenuto: non verrà più proposto.' : `${kept.size} brani tenuti: non verranno più proposti.`}
            </div>
          )}
          {items.length === 0 ? (
            removed.size === 0 && (
              <p className="max-w-[62ch] text-dim">
                {result.stats.candidates === 0
                  ? 'Nessun brano poco ascoltato con questi criteri. Prova a ridurre i giorni minimi.'
                  : 'Nessun brano da eliminare: la tua libreria è in linea con i tuoi gusti.'}
              </p>
            )
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <label className="mr-auto flex cursor-pointer items-center gap-2.5 text-[15px]">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-[#ef6b5b]"
                    checked={allSelected}
                    disabled={removable.length === 0}
                    onChange={() => setSelected(allSelected ? new Set() : new Set(removable.map((i) => i.uri)))}
                  />
                  Seleziona tutti ({removable.length})
                </label>
                <PlaylistButton
                  uris={selected.size ? [...selected] : removable.map((i) => i.uri)}
                  defaultName="Da eliminare (tunescout)"
                  description="Brani da valutare per l'eliminazione, scelti da tunescout"
                  label={selected.size ? `Playlist di backup (${selected.size})` : 'Playlist di backup'}
                />
                <button className="btn-danger" disabled={selected.size === 0} onClick={() => setConfirm(true)}>
                  Rimuovi selezionati ({selected.size})
                </button>
              </div>
              <p className="text-sm tabular-nums text-dim">
                Su {result.stats.saved} brani salvati, {result.stats.candidates} sono poco ascoltati.
              </p>
              <ErrorBox error={actionError} />
              <ul className="-mx-3 space-y-1">
                {items.map((t) => (
                  <TrackCard
                    key={t.uri}
                    track={t}
                    selected={selected.has(t.uri)}
                    dimmed={kept.has(t.uri)}
                    note={`Salvato il ${dateFmt.format(new Date(t.addedAt))}`}
                    leading={
                      <input
                        type="checkbox"
                        aria-label={`Seleziona ${t.title}`}
                        className="mt-1 h-5 w-5 shrink-0 accent-[#ef6b5b] disabled:cursor-not-allowed"
                        checked={selected.has(t.uri)}
                        disabled={kept.has(t.uri)}
                        onChange={() => toggle(t.uri)}
                      />
                    }
                    actions={
                      <label className="flex cursor-pointer items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          className="h-4 w-4 accent-[#f2a93b]"
                          checked={kept.has(t.uri)}
                          disabled={keeping.has(t.uri)}
                          onChange={() => void toggleKeep(t.uri)}
                        />
                        Da tenere: non propormelo più
                      </label>
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
