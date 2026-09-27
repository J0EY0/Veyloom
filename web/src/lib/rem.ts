// rootRem is the size of a rem in pixels: the interface size setting sets
// it.
export function rootRem(): number {
  return parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
}
