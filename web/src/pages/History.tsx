import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api'
import ErrorBox from '../components/ErrorBox'
import Spinner from '../components/Spinner'

const filters = [
  { value: '', label: 'Tutti' },
  { value: 'like', label: 'Mi piace' },
  { value: 'skip', label: 'Scartati' },
] as const

const dateFmt = new Intl.DateTimeFormat('it-IT', { dateStyle: 'short' })

export default function History() {
  const [feedback, setFeedback] = useState<'' | 'like' | 'skip'>('')
  const q = useQuery({ queryKey: ['history', feedback], queryFn: () => api.history(feedback) })

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end gap-4">
        <div className="mr-auto">
          <h1 className="text-2xl font-bold">Storico</h1>
          <p className="mt-1 text-zinc-400">I suggerimenti ricevuti e il tuo feedback.</p>
        </div>
        <div className="flex gap-1 rounded-full border border-zinc-800 p-1" role="tablist">
          {filters.map((f) => (
            <button
              key={f.value}
              role="tab"
              aria-selected={feedback === f.value}
              className={`rounded-full px-3 py-1 text-sm ${
                feedback === f.value ? 'bg-zinc-800 text-white' : 'text-zinc-400 hover:text-white'
              }`}
              onClick={() => setFeedback(f.value)}
            >
              {f.label}
            </button>
          ))}
        </div>
      </header>

      <ErrorBox error={q.error} />
      {q.isPending ? (
        <Spinner />
      ) : q.data && q.data.items.length === 0 ? (
        <p className="text-zinc-400">Ancora nessun suggerimento{feedback && ' con questo filtro'}.</p>
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-zinc-800 text-xs uppercase tracking-wide text-zinc-500">
              <tr>
                <th className="px-4 py-3 font-medium">Brano</th>
                <th className="hidden px-4 py-3 font-medium md:table-cell">Motivo</th>
                <th className="px-4 py-3 font-medium">Feedback</th>
                <th className="hidden px-4 py-3 font-medium sm:table-cell">Data</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/70">
              {q.data?.items.map((s) => (
                <tr key={s.key}>
                  <td className="px-4 py-3">
                    <a href={s.url} target="_blank" rel="noreferrer" className="font-medium text-white hover:underline">
                      {s.title}
                    </a>
                    <div className="text-zinc-400">{s.artist}</div>
                  </td>
                  <td className="hidden max-w-md px-4 py-3 italic text-zinc-400 md:table-cell">{s.reason}</td>
                  <td className="px-4 py-3">
                    {s.feedback === 'like' && <span className="text-emerald-400">♥ Mi piace</span>}
                    {s.feedback === 'skip' && <span className="text-zinc-500">Scartato</span>}
                    {!s.feedback && <span className="text-zinc-600">—</span>}
                  </td>
                  <td className="hidden whitespace-nowrap px-4 py-3 text-zinc-500 sm:table-cell">
                    {dateFmt.format(new Date(s.createdAt))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
