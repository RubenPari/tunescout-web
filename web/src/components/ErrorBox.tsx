import type { ApiError } from '../api'

export default function ErrorBox({ error }: { error: ApiError | Error | null }) {
  if (!error) return null
  const reauth = 'reauth' in error && error.reauth
  return (
    <div role="alert" className="rounded-xl border border-rose-900 bg-rose-950/50 px-4 py-3 text-sm text-rose-200">
      {error.message}
      {reauth && (
        <a href="/api/auth/login" className="ml-2 font-semibold underline">
          Accedi di nuovo
        </a>
      )}
    </div>
  )
}
