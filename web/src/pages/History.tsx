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
    <div className="space-y-8">
      <header className="flex flex-wrap items-end gap-4">
        <div className="mr-auto">
          <h1 className="display text-[clamp(2.25rem,7vw,3.25rem)]">Storico</h1>
          <p className="mt-2 text-dim">I brani che ti sono stati proposti e cosa ne hai pensato.</p>
        </div>
        <div className="flex gap-1 rounded-lg border border-line p-1" role="tablist" aria-label="Filtra per feedback">
          {filters.map((f) => (
            <button
              key={f.value}
              role="tab"
              aria-selected={feedback === f.value}
              className={`rounded-md px-3 py-1 text-sm transition-colors ${
                feedback === f.value ? 'bg-dial font-semibold text-night' : 'text-dim hover:text-fg'
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
        <p className="text-dim">
          {feedback ? 'Nessun brano con questo filtro.' : 'Ancora nessun suggerimento: inizia da Scopri.'}
        </p>
      ) : (
        <div className="-mx-4 overflow-x-auto sm:mx-0">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-line text-sm text-dim">
              <tr>
                <th className="px-4 py-4 font-medium">Brano</th>
                <th className="hidden px-4 py-4 font-medium md:table-cell">Motivo</th>
                <th className="px-4 py-4 font-medium">Feedback</th>
                <th className="hidden px-4 py-4 font-medium sm:table-cell">Data</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line/60">
              {q.data?.items.map((s) => (
                <tr key={s.key}>
                  <td className="px-4 py-4">
                    <a href={s.url} target="_blank" rel="noreferrer" className="font-semibold hover:text-dial">
                      {s.title}
                    </a>
                    <div className="text-dim">{s.artist}</div>
                  </td>
                  <td className="hidden max-w-md px-4 py-4 text-[15px] text-dim md:table-cell">{s.reason}</td>
                  <td className="px-4 py-4">
                    {s.feedback === 'like' && <span className="text-dial">Mi piace</span>}
                    {s.feedback === 'skip' && <span className="text-dim">Scartato</span>}
                    {!s.feedback && <span className="text-dim/60">Nessuno</span>}
                  </td>
                  <td className="hidden whitespace-nowrap px-4 py-4 tabular-nums text-dim sm:table-cell">
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
