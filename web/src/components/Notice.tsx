import type { ReactNode } from 'react'

const tones = {
  alarm: 'border-alarm bg-alarm-soft',
  dial: 'border-dial bg-dial-soft',
}

/** Riquadro di avviso: tono alarm per gli errori, dial per le conferme. */
export default function Notice({
  tone,
  role = tone === 'alarm' ? 'alert' : 'status',
  className = '',
  children,
}: {
  tone: keyof typeof tones
  role?: 'alert' | 'status'
  className?: string
  children: ReactNode
}) {
  return (
    <div role={role} className={`rounded-lg border-l-[3px] px-4 py-3 text-[15px] text-fg ${tones[tone]} ${className}`}>
      {children}
    </div>
  )
}
