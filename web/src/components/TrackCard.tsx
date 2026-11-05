import type { ReactNode } from 'react'
import type { Suggestion } from '../api'

interface Props {
  track: Suggestion
  note?: string
  actions?: ReactNode
  leading?: ReactNode
  dimmed?: boolean
  selected?: boolean
}

export default function TrackCard({ track, note, actions, leading, dimmed, selected }: Props) {
  return (
    <li
      className={`flex gap-4 px-3 py-4 transition-colors sm:gap-5 ${
        selected ? 'rounded-[10px] bg-alarm-soft' : ''
      } ${dimmed ? 'opacity-45' : ''}`}
    >
      {leading}
      <Cover src={track.image} alt={track.album} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-3">
          <a
            href={track.url}
            target="_blank"
            rel="noreferrer"
            className="display truncate text-xl hover:text-dial"
            title="Apri in Spotify"
          >
            {track.title}
          </a>
          {track.year && <span className="shrink-0 text-sm tabular-nums text-dim">{track.year}</span>}
        </div>
        <p className="truncate text-[15px] text-dim">
          <span className="text-fg">{track.artist}</span>
          {track.album && `, ${track.album}`}
        </p>
        {track.reason && <p className="mt-2 max-w-[62ch] text-[15px] leading-relaxed text-fg/90">{track.reason}</p>}
        {note && <p className="mt-1 text-sm text-dim">{note}</p>}
        {actions && <div className="mt-3 flex flex-wrap gap-2">{actions}</div>}
      </div>
    </li>
  )
}

function Cover({ src, alt }: { src: string; alt: string }) {
  const size = 'h-20 w-20 shrink-0 rounded-[3px] sm:h-24 sm:w-24'
  if (!src) {
    return (
      <div className={`${size} flex items-center justify-center bg-panel text-line`} aria-hidden="true">
        <svg viewBox="0 0 24 24" className="h-8 w-8" fill="currentColor">
          <path d="M9 18.5a3 3 0 1 1-2-2.83V5l12-2.5v12a3 3 0 1 1-2-2.83V6.2l-8 1.66z" />
        </svg>
      </div>
    )
  }
  return <img src={src} alt={alt ? `Copertina di ${alt}` : ''} loading="lazy" className={`${size} object-cover`} />
}
