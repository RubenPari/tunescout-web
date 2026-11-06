/** Sceglie singolare o plurale: plural(2, 'brano', 'brani') → 'brani'. */
export function plural(n: number, one: string, many: string): string {
  return n === 1 ? one : many
}

/** Esce dall'app dopo logout o eliminazione dell'account: dimentica lo stato locale e ricarica. */
export function leaveApp() {
  sessionStorage.clear()
  window.location.href = '/'
}

/** Restituisce una copia di set con x aggiunto (on = true) o tolto (on = false). */
export function withItem<T>(set: Set<T>, x: T, on: boolean): Set<T> {
  const n = new Set(set)
  if (on) n.add(x)
  else n.delete(x)
  return n
}
