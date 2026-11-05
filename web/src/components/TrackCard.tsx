import type { ReactNode } from 'react'
import type { Suggestion } from '../api'

interface Props {
  track: Suggestion
  note?: string
  actions?: ReactNode
  leading?: ReactNode
  dimmed?: boolean
}

export default function TrackCard({ track, note, actions, leading, dimmed }: Props) {
  return (
    <li className={`card flex gap-4 p-3 transition sm:p-4 ${dimmed ? 'opacity-50' : ''}`}>
      {leading}
      <Cover src={track.image} alt={track.album} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <a
            href={track.url}
            target="_blank"
            rel="noreferrer"
            className="truncate font-semibold text-white hover:underline"
            title="Apri in Spotify"
          >
            {track.title}
          </a>
          {track.year && <span className="text-xs text-zinc-500">{track.year}</span>}
        </div>
        <p className="truncate text-sm text-zinc-300">
          {track.artist}
          {track.album && <span className="text-zinc-500"> · {track.album}</span>}
        </p>
        {track.reason && <p className="mt-1.5 text-sm italic text-emerald-300/90">{track.reason}</p>}
        {note && <p className="mt-1 text-xs text-zinc-500">{note}</p>}
        {actions && <div className="mt-3 flex flex-wrap gap-2">{actions}</div>}
      </div>
    </li>
  )
}

function Cover({ src, alt }: { src: string; alt: string }) {
  if (!src) {
    return (
      <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-lg bg-zinc-800 text-2xl text-zinc-600 sm:h-24 sm:w-24">
        ♪
      </div>
    )
  }
  return (
    <img
      src={src}
      alt={alt ? `Copertina di ${alt}` : ''}
      loading="lazy"
      className="h-20 w-20 shrink-0 rounded-lg object-cover sm:h-24 sm:w-24"
    />
  )
}
