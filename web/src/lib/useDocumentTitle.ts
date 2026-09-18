import { useEffect } from 'react'

// Keeps the tab title in step with the page; an empty title shows the app name alone.
export function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title ? `${title} · Veyloom` : 'Veyloom'
  }, [title])
}
