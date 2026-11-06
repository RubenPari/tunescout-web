import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError, queryKeys, toApiError, type Job, type JobKind } from './api'

export interface JobState<R> {
  job: Job<R> | null
  logs: string[]
  running: boolean
  error: ApiError | null
  start: (params?: object) => Promise<void>
  reset: () => void
}

const storageKey = (kind: JobKind) => `tunescout:job:${kind}`

/**
 * Avvia un job sul server e ne segue il progresso via Server-Sent Events.
 * L'id dell'ultimo job viene salvato in sessionStorage, così il risultato
 * resta visibile cambiando pagina (e un job in corso viene ripreso).
 */
export function useJob<R>(kind: JobKind): JobState<R> {
  const qc = useQueryClient()
  const [job, setJob] = useState<Job<R> | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const esRef = useRef<EventSource | null>(null)

  const follow = useCallback(
    (id: string) => {
      esRef.current?.close()
      setRunning(true)
      setLogs([])
      const es = new EventSource(`/api/jobs/${id}/events`)
      esRef.current = es
      es.addEventListener('log', (e) => {
        const { message } = JSON.parse((e as MessageEvent).data) as { message: string }
        setLogs((l) => [...l, message])
      })
      es.addEventListener('done', (e) => {
        const { job } = JSON.parse((e as MessageEvent).data) as { job: Job<R> }
        es.close()
        setJob(job)
        setRunning(false)
        if (job.status === 'failed') setError(new ApiError(500, job.error ?? 'Operazione non riuscita'))
        void qc.invalidateQueries({ queryKey: queryKeys.me })
        void qc.invalidateQueries({ queryKey: queryKeys.history })
      })
      es.onerror = () => {
        // EventSource si riconnette da solo; se il job non esiste più, chiudi.
        if (es.readyState === EventSource.CLOSED) {
          setRunning(false)
          sessionStorage.removeItem(storageKey(kind))
        }
      }
    },
    [kind, qc],
  )

  useEffect(() => {
    const id = sessionStorage.getItem(storageKey(kind))
    if (id) follow(id)
    return () => esRef.current?.close()
  }, [kind, follow])

  const start = useCallback(
    async (params: object = {}) => {
      setError(null)
      setJob(null)
      try {
        const j = await api.startJob(kind, params)
        sessionStorage.setItem(storageKey(kind), j.id)
        void qc.invalidateQueries({ queryKey: queryKeys.me })
        follow(j.id)
      } catch (e) {
        setError(toApiError(e))
      }
    },
    [kind, follow, qc],
  )

  const reset = useCallback(() => {
    esRef.current?.close()
    sessionStorage.removeItem(storageKey(kind))
    setJob(null)
    setLogs([])
    setError(null)
    setRunning(false)
  }, [kind])

  return { job, logs, running, error, start, reset }
}
