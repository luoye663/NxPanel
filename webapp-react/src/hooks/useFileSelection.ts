import { useMemo, useState } from 'react'

export function useFileSelection() {
  const [selected, setSelected] = useState<string[]>([])
  const selectedSet = useMemo(() => new Set(selected), [selected])

  function toggle(name: string) {
    setSelected((current) => current.includes(name) ? current.filter((item) => item !== name) : [...current, name])
  }

  function toggleAll(names: string[], checked: boolean) {
    setSelected(checked ? names : [])
  }

  return { selected, selectedSet, setSelected, toggle, toggleAll, clear: () => setSelected([]) }
}
