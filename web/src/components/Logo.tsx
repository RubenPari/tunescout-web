/** Marchio: una manopola con la tacca ambra. */
export default function Logo({ className = 'h-7 w-7' }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 32 32" aria-hidden="true">
      <circle cx="16" cy="16" r="15" fill="var(--color-panel)" stroke="var(--color-line)" strokeWidth="2" />
      <circle cx="16" cy="16" r="9" fill="var(--color-night)" />
      <path d="M16 3.5v8" stroke="var(--color-dial)" strokeWidth="3" strokeLinecap="round" transform="rotate(38 16 16)" />
    </svg>
  )
}
