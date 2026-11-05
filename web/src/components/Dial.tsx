import { useLayoutEffect, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router'

export interface Station {
  to: string
  label: string
  end?: boolean
}

/**
 * Navigazione come scala di sintonia: l'ago si sposta sulla sezione attiva
 * e, mentre un'operazione è in corso, scorre la scala cercando una stazione.
 */
export default function Dial({ stations, scanning }: { stations: Station[]; scanning: boolean }) {
  const { pathname } = useLocation()
  const listRef = useRef<HTMLUListElement>(null)
  const [needle, setNeedle] = useState<{ x: number; ready: boolean } | null>(null)

  useLayoutEffect(() => {
    const list = listRef.current
    if (!list) return
    const place = () => {
      const active = list.querySelector<HTMLElement>('a[aria-current="page"]')
      if (!active) return setNeedle(null)
      const x = active.offsetLeft + active.offsetWidth / 2
      setNeedle((n) => ({ x, ready: n !== null }))
    }
    place()
    const ro = new ResizeObserver(place)
    ro.observe(list)
    return () => ro.disconnect()
  }, [pathname])

  return (
    <nav aria-label="Sezioni" className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
      <div className="relative min-w-max pb-[18px]">
        <ul ref={listRef} className="flex gap-1 sm:gap-6">
          {stations.map((s) => (
            <li key={s.to}>
              <NavLink
                to={s.to}
                end={s.end}
                className={({ isActive }) =>
                  `block px-3 py-2 text-[15px] transition-colors ${
                    isActive ? 'font-semibold text-fg' : 'text-dim hover:text-fg'
                  }`
                }
              >
                {s.label}
              </NavLink>
            </li>
          ))}
        </ul>
        <div aria-hidden="true" className="ticks absolute inset-x-0 bottom-0 h-[14px] border-b border-line" />
        {needle && (
          <span
            aria-hidden="true"
            className={`absolute bottom-0 h-[22px] w-[3px] -translate-x-1/2 rounded-full bg-dial shadow-[0_0_12px_var(--color-dial)] ${
              needle.ready ? 'transition-[left] duration-500 ease-[cubic-bezier(0.3,0,0.2,1)]' : ''
            } ${scanning ? 'needle-scan' : ''}`}
            style={{ left: needle.x }}
          />
        )}
        {scanning && <span className="sr-only" role="status">Operazione in corso</span>}
      </div>
    </nav>
  )
}

/** Scala decorativa con le frequenze, per la pagina di accesso. */
export function Band() {
  const freqs = [88, 92, 96, 100, 104, 108]
  return (
    <div aria-hidden="true" className="relative pb-[30px]">
      <div className="flex justify-between text-sm tabular-nums text-dim">
        {freqs.map((f) => (
          <span key={f}>{f}</span>
        ))}
      </div>
      <div className="ticks absolute inset-x-0 bottom-0 h-[22px] border-b border-line [background-size:100%_22px,100%_10px]" />
      <span className="needle-tune absolute bottom-0 h-[38px] w-[3px] -translate-x-1/2 rounded-full bg-dial shadow-[0_0_16px_var(--color-dial)]" />
    </div>
  )
}
