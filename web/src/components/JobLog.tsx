import { useEffect, useRef } from 'react'
import Spinner from './Spinner'

export default function JobLog({ logs, running }: { logs: string[]; running: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    ref.current?.scrollTo({ top: ref.current.scrollHeight })
  }, [logs])
  if (!running && logs.length === 0) return null
  return (
    <div className="card overflow-hidden">
      {running && (
        <div className="flex items-center gap-3 border-b border-zinc-800 px-4 py-3 text-sm text-zinc-300">
          <Spinner className="h-4 w-4" />
          {logs.at(-1)?.trim() || 'Avvio…'}
        </div>
      )}
      <div ref={ref} className="max-h-48 overflow-y-auto px-4 py-3 font-mono text-xs leading-relaxed text-zinc-500">
        {logs.map((l, i) => (
          <div key={i} className="whitespace-pre-wrap">
            {l}
          </div>
        ))}
      </div>
    </div>
  )
}
