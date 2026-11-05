import { useEffect, useRef } from 'react'
import Spinner from './Spinner'

export default function JobLog({ logs, running }: { logs: string[]; running: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    ref.current?.scrollTo({ top: ref.current.scrollHeight })
  }, [logs])
  if (!running && logs.length === 0) return null
  return (
    <div className="panel">
      {running && (
        <div className="flex items-center gap-3 px-4 py-3 text-[15px]" aria-live="polite">
          <Spinner className="h-4 w-4 shrink-0" />
          {logs.at(-1)?.trim() || 'Avvio…'}
        </div>
      )}
      {logs.length > 0 && (
        <details className={`group px-4 py-3 ${running ? 'border-t border-line' : ''}`}>
          <summary className="cursor-pointer text-sm text-dim hover:text-fg">
            {running ? 'Dettagli' : `Dettagli dell'operazione (${logs.length} passaggi)`}
          </summary>
          <div ref={ref} className="mt-2 max-h-56 overflow-y-auto font-mono text-xs leading-relaxed text-dim">
            {logs.map((l, i) => (
              <div key={i} className="whitespace-pre-wrap">
                {l}
              </div>
            ))}
          </div>
        </details>
      )}
    </div>
  )
}
