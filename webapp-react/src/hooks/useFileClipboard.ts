import { useState } from 'react'

export interface FileClipboardState {
  mode: 'copy' | 'cut'
  paths: string[]
  sourceDir: string
}

export function useFileClipboard() {
  const [clipboard, setClipboard] = useState<FileClipboardState | null>(null)

  return {
    clipboard,
    copy: (paths: string[], sourceDir: string) => setClipboard({ mode: 'copy', paths, sourceDir }),
    cut: (paths: string[], sourceDir: string) => setClipboard({ mode: 'cut', paths, sourceDir }),
    clearClipboard: () => setClipboard(null),
  }
}
