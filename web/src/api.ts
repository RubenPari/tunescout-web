// Client tipizzato per l'API di tunescout-web.

export interface User {
  id: number
  spotifyId: string
  displayName: string
  createdAt: string
}

export type Feedback = 'like' | 'skip'

export interface Suggestion {
  key: string
  spotifyId: string
  uri: string
  artist: string
  title: string
  album: string
  year: string
  reason: string
  url: string
  image: string
  createdAt: string
  feedback: '' | Feedback
}

export type JobKind = 'sync' | 'discover' | 'prune'

export interface Job<R = unknown> {
  id: string
  kind: JobKind
  status: 'running' | 'done' | 'failed'
  params: unknown
  result?: R
  error?: string
  createdAt: string
  finishedAt?: string
}

export interface Me {
  user: User
  library: { lastSync: string | null; tracks: number; artists: number }
  usage: { today: number; limit: number }
  activeJob?: Job
}

export interface DiscoverParams {
  count: number
  mood: string
  genres: string[]
  newArtists: boolean
}

export interface DiscoverResult {
  suggestions: Suggestion[]
  stats: { proposed: number; alreadyKnown: number; knownArtist: number; notFound: number; rounds: number }
  requested: number
}

export interface PruneParams {
  count: number
  minAgeDays: number
}

export interface PruneItem extends Suggestion {
  addedAt: string
}

export interface PruneResult {
  items: PruneItem[]
  stats: { saved: number; candidates: number }
}

export interface SyncResult {
  tracks: number
  artists: number
}

export interface Playlist {
  id: string
  name: string
  url: string
  tracks: number
}

export class ApiError extends Error {
  status: number
  reauth: boolean

  constructor(status: number, message: string, reauth = false) {
    super(message)
    this.status = status
    this.reauth = reauth
  }
}

/** Normalizza un errore qualsiasi (rete, eccezioni) in ApiError. */
export function toApiError(e: unknown): ApiError {
  return e instanceof ApiError ? e : new ApiError(0, String(e))
}

/** Chiavi di React Query condivise tra le pagine. */
export const queryKeys = {
  me: ['me'],
  history: ['history'],
} as const

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  let data: unknown = null
  try {
    data = await res.json()
  } catch {
    // risposta senza JSON
  }
  if (!res.ok) {
    const err = (data ?? {}) as { error?: string; reauth?: boolean }
    throw new ApiError(res.status, err.error ?? `Errore HTTP ${res.status}`, err.reauth ?? false)
  }
  return data as T
}

export const api = {
  me: () => request<Me>('GET', '/api/me'),
  logout: () => request<void>('POST', '/api/auth/logout'),
  deleteAccount: () => request<void>('DELETE', '/api/me'),

  startJob: (kind: JobKind, params: object = {}) => request<Job>('POST', `/api/jobs/${kind}`, params),
  job: <R>(id: string) => request<Job<R>>('GET', `/api/jobs/${id}`),

  history: (feedback: '' | Feedback, limit = 200) =>
    request<{ items: Suggestion[] }>('GET', `/api/history?feedback=${feedback}&limit=${limit}`),
  feedback: (keys: string[], feedback: Feedback) =>
    request<{ updated: number }>('POST', '/api/feedback', { keys, feedback }),
  createPlaylist: (name: string, uris: string[], description = '') =>
    request<Playlist>('POST', '/api/playlists', { name, uris, description }),
  pruneRemove: (jobId: string, uris: string[]) =>
    request<{ removed: number }>('POST', `/api/jobs/${jobId}/remove`, { uris }),
  pruneKeep: (jobId: string, uri: string, keep: boolean) =>
    request<{ kept: boolean }>(keep ? 'POST' : 'DELETE', `/api/jobs/${jobId}/keep`, { uri }),
}
