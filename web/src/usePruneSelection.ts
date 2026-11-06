import { useState } from 'react'
import { api, toApiError, type ApiError } from './api'
import { withItem } from './util'

/**
 * Stato delle azioni sui brani proposti da un job prune: selezione, brani
 * rimossi, brani da tenere (con le richieste in corso) ed errore dell'ultima azione.
 */
export function usePruneSelection(jobId: string | undefined) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [removed, setRemoved] = useState<Set<string>>(new Set())
  const [kept, setKept] = useState<Set<string>>(new Set())
  const [keeping, setKeeping] = useState<Set<string>>(new Set())
  const [removing, setRemoving] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const reset = () => {
    setSelected(new Set())
    setRemoved(new Set())
    setKept(new Set())
    setError(null)
  }

  const toggle = (uri: string) => setSelected((s) => withItem(s, uri, !s.has(uri)))

  const toggleKeep = async (uri: string) => {
    if (!jobId || keeping.has(uri)) return
    const keep = !kept.has(uri)
    setKeeping((k) => withItem(k, uri, true))
    setError(null)
    try {
      await api.pruneKeep(jobId, uri, keep)
      setKept((k) => withItem(k, uri, keep))
      if (keep) setSelected((s) => withItem(s, uri, false))
    } catch (e) {
      setError(toApiError(e))
    } finally {
      setKeeping((k) => withItem(k, uri, false))
    }
  }

  /** Rimuove dalla libreria i brani selezionati. */
  const remove = async () => {
    if (!jobId) return
    setRemoving(true)
    setError(null)
    try {
      const uris = [...selected]
      await api.pruneRemove(jobId, uris)
      setRemoved((r) => new Set([...r, ...uris]))
      setSelected(new Set())
    } catch (e) {
      setError(toApiError(e))
    } finally {
      setRemoving(false)
    }
  }

  return { selected, setSelected, removed, kept, keeping, removing, error, reset, toggle, toggleKeep, remove }
}
