import { useState } from 'react'
import { api, ApiError, type DiscoverParams, type DiscoverResult, type Suggestion } from '../api'
import ErrorBox from '../components/ErrorBox'
import JobLog from '../components/JobLog'
import PlaylistButton from '../components/PlaylistButton'
import TagInput from '../components/TagInput'
import TrackCard from '../components/TrackCard'
import { useJob } from '../useJob'

type Feedback = Record<string, 'like' | 'skip' | 'pending'>

export default function Discover() {
  const job = useJob<DiscoverResult>('discover')
  const [form, setForm] = useState<DiscoverParams>({ count: 20, mood: '', genres: [], newArtists: false })
  const [feedback, setFeedback] = useState<Feedback>({})
  const [actionError, setActionError] = useState<ApiError | null>(null)

  const result = job.job?.status === 'done' ? job.job.result : undefined
  const tracks = result?.suggestions ?? []

  const submit = () => {
    setFeedback({})
    setActionError(null)
    void job.start(form)
  }

  const send = async (list: Suggestion[], fb: 'like' | 'skip') => {
    const keys = list.map((s) => s.key)
    const prev = { ...feedback }
    setActionError(null)
    setFeedback((f) => ({ ...f, ...Object.fromEntries(keys.map((k) => [k, 'pending' as const])) }))
    try {
      await api.feedback(keys, fb)
      setFeedback((f) => ({ ...f, ...Object.fromEntries(keys.map((k) => [k, fb])) }))
    } catch (e) {
      setFeedback(prev)
      setActionError(e instanceof ApiError ? e : new ApiError(0, String(e)))
    }
  }

  const unsaved = tracks.filter((t) => feedback[t.key] !== 'like')

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-bold">Scopri</h1>
        <p className="mt-1 text-zinc-400">
          Claude propone brani in base al tuo profilo; quelli che non esistono su Spotify, che hai già o che ti sono già
          stati suggeriti vengono scartati.
        </p>
      </header>

      <form
        className="card grid gap-4 p-5 sm:grid-cols-[8rem_1fr]"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <div>
          <label className="label" htmlFor="count">
            Brani
          </label>
          <input
            id="count"
            className="input"
            type="number"
            min={1}
            max={50}
            value={form.count}
            onChange={(e) => setForm({ ...form, count: Math.max(1, Math.min(50, Number(e.target.value) || 1)) })}
          />
        </div>
        <div>
          <label className="label" htmlFor="mood">
            Mood (facoltativo)
          </label>
          <input
            id="mood"
            className="input"
            maxLength={200}
            placeholder="es. notturno, malinconico · energico · per studiare"
            value={form.mood}
            onChange={(e) => setForm({ ...form, mood: e.target.value })}
          />
        </div>
        <div className="sm:col-span-2">
          <label className="label" htmlFor="genres">
            Generi (facoltativi, Invio per aggiungere)
          </label>
          <TagInput
            id="genres"
            value={form.genres}
            onChange={(genres) => setForm({ ...form, genres })}
            placeholder="es. shoegaze, dream pop"
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-4 sm:col-span-2">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              className="h-4 w-4 accent-emerald-500"
              checked={form.newArtists}
              onChange={(e) => setForm({ ...form, newArtists: e.target.checked })}
            />
            Solo artisti che non ho in libreria
          </label>
          <button className="btn-primary" disabled={job.running}>
            {job.running ? 'Cerco…' : 'Trova brani'}
          </button>
        </div>
      </form>

      <ErrorBox error={job.error} />
      <JobLog logs={job.logs} running={job.running} />

      {result && (
        <section className="space-y-4">
          {tracks.length === 0 ? (
            <p className="text-zinc-400">Nessun brano nuovo trovato: prova con meno vincoli o riprova.</p>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="mr-auto text-lg font-semibold">
                  {tracks.length} {tracks.length === 1 ? 'brano' : 'brani'}
                  {tracks.length < result.requested && (
                    <span className="ml-2 text-sm font-normal text-zinc-500">su {result.requested} richiesti</span>
                  )}
                </h2>
                <button className="btn-ghost" disabled={unsaved.length === 0} onClick={() => send(unsaved, 'like')}>
                  Salva tutti in libreria
                </button>
                <PlaylistButton
                  uris={tracks.map((t) => t.uri)}
                  defaultName="tunescout · scoperte"
                  description="Suggerimenti generati da tunescout"
                />
              </div>
              <ErrorBox error={actionError} />
              <ul className="space-y-3">
                {tracks.map((t) => (
                  <TrackCard
                    key={t.key}
                    track={t}
                    dimmed={feedback[t.key] === 'skip'}
                    actions={<Actions state={feedback[t.key]} onLike={() => send([t], 'like')} onSkip={() => send([t], 'skip')} url={t.url} />}
                  />
                ))}
              </ul>
              <p className="text-xs text-zinc-500">
                Like e Scarta vengono usati come segnale per i suggerimenti successivi.
              </p>
            </>
          )}
        </section>
      )}
    </div>
  )
}

function Actions({
  state,
  onLike,
  onSkip,
  url,
}: {
  state?: 'like' | 'skip' | 'pending'
  onLike: () => void
  onSkip: () => void
  url: string
}) {
  const pending = state === 'pending'
  return (
    <>
      <button
        className={state === 'like' ? 'btn bg-emerald-500/20 px-3 py-1 text-emerald-300' : 'btn-ghost px-3 py-1'}
        disabled={pending || state === 'like'}
        onClick={onLike}
      >
        {state === 'like' ? '♥ Salvato' : '♡ Mi piace'}
      </button>
      <button
        className={state === 'skip' ? 'btn bg-zinc-800 px-3 py-1 text-zinc-400' : 'btn-ghost px-3 py-1'}
        disabled={pending || state === 'skip'}
        onClick={onSkip}
      >
        {state === 'skip' ? 'Scartato' : 'Scarta'}
      </button>
      <a className="btn-ghost px-3 py-1" href={url} target="_blank" rel="noreferrer">
        Apri in Spotify
      </a>
    </>
  )
}
