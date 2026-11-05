export default function Logo({ className = 'h-7 w-7' }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 32 32" aria-hidden="true">
      <rect width="32" height="32" rx="8" fill="#10b981" />
      <path d="M12 22.5a3 3 0 1 1-2-2.83V9l12-2.5v12a3 3 0 1 1-2-2.83V10.2l-8 1.66z" fill="#0b1210" />
    </svg>
  )
}
