// The project last chosen on the Wiki page of the sidebar (docs/design.md
// 5.18), for the page to open at again; none for every project's. Kept per
// browser, like the theme: a choice of view, nothing the hub needs.

const STORAGE_KEY = 'veyloom.wikiProject'

export function lastWiki(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function rememberWiki(projectId: string) {
  try {
    if (projectId) localStorage.setItem(STORAGE_KEY, projectId)
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Then the page opens at every project's.
  }
}
