import type { ReactNode } from 'react'

/** Titolo di pagina con descrizione opzionale. */
export default function PageHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <>
      <h1 className="display text-[clamp(2.25rem,7vw,3.25rem)]">{title}</h1>
      {children}
    </>
  )
}
