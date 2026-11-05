import type { ApiError } from '../api'

export default function ErrorBox({ error }: { error: ApiError | Error | null }) {
  if (!error) return null
  const reauth = 'reauth' in error && error.reauth
  return (
    <div role="alert" className="rounded-lg border-l-[3px] border-alarm bg-alarm-soft px-4 py-3 text-[15px] text-fg">
      {error.message}
      {reauth && (
        <a href="/api/auth/login" className="ml-2 font-semibold text-alarm underline underline-offset-2">
          Accedi di nuovo
        </a>
      )}
    </div>
  )
}
