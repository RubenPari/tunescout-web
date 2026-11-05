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
    <div className="space-y-8">
      <header>
        <h1 className="display text-[clamp(2.25rem,7vw,3.25rem)]">Scopri</h1>
        <p className="mt-2 max-w-[62ch] text-dim">
          Claude propone brani in base al tuo profilo. Quelli che non esistono su Spotify, che hai già o che ti sono
          già stati suggeriti vengono scartati.
        </p>
      </header>

      <form
        className="panel grid gap-5 p-5 sm:grid-cols-[8rem_1fr] sm:p-6"
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
            Mood, se vuoi
          </label>
          <input
            id="mood"
            className="input"
            maxLength={200}
            placeholder="es. notturno e malinconico, oppure per studiare"
            value={form.mood}
            onChange={(e) => setForm({ ...form, mood: e.target.value })}
          />
        </div>
        <div className="sm:col-span-2">
          <label className="label" htmlFor="genres">
            Generi, se vuoi (premi Invio per aggiungerne uno)
          </label>
          <TagInput
            id="genres"
            value={form.genres}
            onChange={(genres) => setForm({ ...form, genres })}
            placeholder="es. shoegaze, dream pop"
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-4 sm:col-span-2">
          <label className="flex cursor-pointer items-center gap-2.5 text-[15px]">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[#f2a93b]"
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
            <p className="text-dim">
              Nessun brano nuovo trovato. Togli qualche genere o il mood e cerca di nuovo.
            </p>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="display mr-auto text-2xl tabular-nums">
                  {tracks.length} {tracks.length === 1 ? 'brano' : 'brani'}
                  {tracks.length < result.requested && (
                    <span className="ml-2 text-base font-normal tracking-normal text-dim [font-stretch:100%]">su {result.requested} richiesti</span>
                  )}
                </h2>
                <button className="btn-ghost" disabled={unsaved.length === 0} onClick={() => send(unsaved, 'like')}>
                  Salva tutti in libreria
                </button>
                <PlaylistButton
                  uris={tracks.map((t) => t.uri)}
                  defaultName="Scoperte di tunescout"
                  description="Suggerimenti generati da tunescout"
                />
              </div>
              <ErrorBox error={actionError} />
              <ul className="-mx-3 divide-y divide-line/60">
                {tracks.map((t) => (
                  <TrackCard
                    key={t.key}
                    track={t}
                    dimmed={feedback[t.key] === 'skip'}
                    actions={<Actions state={feedback[t.key]} onLike={() => send([t], 'like')} onSkip={() => send([t], 'skip')} url={t.url} />}
                  />
                ))}
              </ul>
              <p className="text-sm text-dim">
                Mi piace e Scarta orientano i suggerimenti successivi. Mi piace salva anche il brano nella tua libreria.
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
        className={state === 'like' ? 'btn bg-dial-soft px-3 py-1 text-dial' : 'btn-ghost px-3 py-1'}
        disabled={pending || state === 'like'}
        onClick={onLike}
      >
        {state === 'like' ? 'Salvato in libreria' : 'Mi piace'}
      </button>
      <button
        className={state === 'skip' ? 'btn bg-panel px-3 py-1 text-dim' : 'btn-ghost px-3 py-1'}
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
