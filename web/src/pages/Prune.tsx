import { useState } from 'react'
import type { PruneItem, PruneParams, PruneResult } from '../api'
import ConfirmDialog from '../components/ConfirmDialog'
import ErrorBox from '../components/ErrorBox'
import JobLog from '../components/JobLog'
import Notice from '../components/Notice'
import NumberField from '../components/NumberField'
import PageHeader from '../components/PageHeader'
import PlaylistButton from '../components/PlaylistButton'
import TrackCard from '../components/TrackCard'
import { useJob } from '../useJob'
import { usePruneSelection } from '../usePruneSelection'
import { plural } from '../util'

const dateFmt = new Intl.DateTimeFormat('it-IT', { dateStyle: 'medium' })

export default function Prune() {
  const job = useJob<PruneResult>('prune')
  const [form, setForm] = useState<PruneParams>({ count: 20, minAgeDays: 90 })
  const sel = usePruneSelection(job.job?.id)
  const { selected, removed, kept } = sel
  const [confirm, setConfirm] = useState(false)

  const result = job.job?.status === 'done' ? job.job.result : undefined
  const items = (result?.items ?? []).filter((i) => !removed.has(i.uri))
  const removable = items.filter((i) => !kept.has(i.uri))

  const submit = () => {
    sel.reset()
    void job.start(form)
  }

  const remove = async () => {
    await sel.remove()
    setConfirm(false)
  }

  const allSelected = removable.length > 0 && removable.every((i) => selected.has(i.uri))

  return (
    <div className="space-y-8">
      <header>
        <PageHeader title="Fai pulizia">
          <p className="mt-2 max-w-[62ch] text-dim">
            Spotify non dice quante volte hai ascoltato un brano. Per questo un brano conta come poco ascoltato se è
            salvato da un certo numero di giorni, non è tra i tuoi brani più ascoltati e nessuno dei suoi artisti è tra
            i 50 che ascolti di più. Tra questi, Claude sceglie quelli più lontani dai tuoi gusti.
          </p>
          <p className="mt-2 text-dim">
            Nulla viene rimosso senza la tua conferma. I brani che segni come da tenere non ti verranno più proposti.
          </p>
        </PageHeader>
      </header>

      <form
        className="panel flex flex-wrap items-end gap-5 p-5 sm:p-6"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <div className="w-32">
          <NumberField
            id="pcount"
            label="Brani"
            min={1}
            max={50}
            value={form.count}
            onChange={(count) => setForm({ ...form, count })}
          />
        </div>
        <div className="w-52">
          <NumberField
            id="minage"
            label="Salvati da almeno, in giorni"
            min={0}
            max={3650}
            value={form.minAgeDays}
            onChange={(minAgeDays) => setForm({ ...form, minAgeDays })}
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
            <Notice tone="dial">
              {removed.size === 1 ? 'Rimosso 1 brano' : `Rimossi ${removed.size} brani`} dalla libreria. Scopri non li
              proporrà più.
            </Notice>
          )}
          {kept.size > 0 && (
            <Notice tone="dial">
              {kept.size === 1 ? '1 brano tenuto: non verrà più proposto.' : `${kept.size} brani tenuti: non verranno più proposti.`}
            </Notice>
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
                    className="h-4 w-4 accent-alarm"
                    checked={allSelected}
                    disabled={removable.length === 0}
                    onChange={() => sel.setSelected(allSelected ? new Set() : new Set(removable.map((i) => i.uri)))}
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
              <ErrorBox error={sel.error} />
              <ul className="-mx-3 space-y-1">
                {items.map((t) => (
                  <PruneRow
                    key={t.uri}
                    item={t}
                    selected={selected.has(t.uri)}
                    kept={kept.has(t.uri)}
                    keeping={sel.keeping.has(t.uri)}
                    onToggle={() => sel.toggle(t.uri)}
                    onToggleKeep={() => void sel.toggleKeep(t.uri)}
                  />
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      <ConfirmDialog
        open={confirm}
        title={`Rimuovere ${selected.size} ${plural(selected.size, 'brano', 'brani')} dalla libreria?`}
        confirmLabel={sel.removing ? 'Rimuovo…' : 'Rimuovi'}
        danger
        busy={sel.removing}
        onConfirm={remove}
        onCancel={() => setConfirm(false)}
      >
        I brani verranno tolti dai "Brani che ti piacciono" su Spotify. Se vuoi conservarne una copia, crea prima una
        playlist di backup.
      </ConfirmDialog>
    </div>
  )
}

function PruneRow({
  item,
  selected,
  kept,
  keeping,
  onToggle,
  onToggleKeep,
}: {
  item: PruneItem
  selected: boolean
  kept: boolean
  keeping: boolean
  onToggle: () => void
  onToggleKeep: () => void
}) {
  return (
    <TrackCard
      track={item}
      selected={selected}
      dimmed={kept}
      note={`Salvato il ${dateFmt.format(new Date(item.addedAt))}`}
      leading={
        <input
          type="checkbox"
          aria-label={`Seleziona ${item.title}`}
          className="mt-1 h-5 w-5 shrink-0 accent-alarm disabled:cursor-not-allowed"
          checked={selected}
          disabled={kept}
          onChange={onToggle}
        />
      }
      actions={
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input type="checkbox" className="h-4 w-4 accent-dial" checked={kept} disabled={keeping} onChange={onToggleKeep} />
          Da tenere: non propormelo più
        </label>
      }
    />
  )
}
