import type { ApiError } from '../api'
import Notice from './Notice'

export default function ErrorBox({ error }: { error: ApiError | Error | null }) {
  if (!error) return null
  const reauth = 'reauth' in error && error.reauth
  return (
    <Notice tone="alarm">
      {error.message}
      {reauth && (
        <a href="/api/auth/login" className="ml-2 font-semibold text-alarm underline underline-offset-2">
          Accedi di nuovo
        </a>
      )}
    </Notice>
  )
}
