import { useState, type KeyboardEvent } from 'react'

interface Props {
  id?: string
  value: string[]
  onChange: (v: string[]) => void
  placeholder?: string
  max?: number
}

export default function TagInput({ id, value, onChange, placeholder, max = 10 }: Props) {
  const [text, setText] = useState('')
  const add = () => {
    const t = text.trim().replace(/,$/, '').trim()
    if (t && !value.includes(t) && value.length < max) onChange([...value, t])
    setText('')
  }
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      add()
    } else if (e.key === 'Backspace' && !text && value.length) {
      onChange(value.slice(0, -1))
    }
  }
  return (
    <div className="input flex flex-wrap items-center gap-1.5 py-1.5">
      {value.map((g) => (
        <span key={g} className="flex items-center gap-1 rounded-md bg-dial-soft py-0.5 pl-2.5 pr-1.5 text-sm text-dial">
          {g}
          <button
            type="button"
            aria-label={`Rimuovi ${g}`}
            className="rounded px-1 text-dial/80 hover:text-fg"
            onClick={() => onChange(value.filter((x) => x !== g))}
          >
            ×
          </button>
        </span>
      ))}
      <input
        id={id}
        className="min-w-24 flex-1 bg-transparent py-0.5 outline-none placeholder-dim/70"
        value={text}
        maxLength={50}
        placeholder={value.length ? '' : placeholder}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKey}
        onBlur={add}
        disabled={value.length >= max}
      />
    </div>
  )
}
