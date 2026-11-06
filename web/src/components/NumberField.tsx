interface Props {
  id: string
  label: string
  value: number
  min: number
  max: number
  onChange: (v: number) => void
}

/** Campo numerico con etichetta; il valore resta sempre tra min e max. */
export default function NumberField({ id, label, value, min, max, onChange }: Props) {
  return (
    <div>
      <label className="label" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        className="input"
        type="number"
        min={min}
        max={max}
        value={value}
        onChange={(e) => onChange(Math.max(min, Math.min(max, Number(e.target.value) || min)))}
      />
    </div>
  )
}
