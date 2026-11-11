import {useEffect, useState, useSyncExternalStore} from 'react'

const STORAGE_KEY = 'earnest.pane-widths'
const NAVIGATION = {min: 200, max: 480, initial: 248}
const LIST = {min: 304, max: 720, initial: 432}
const READER_MIN = 320

type Widths = {navigation: number; list: number}

function clamp(width: number, min: number, max: number) {
  return Math.round(Math.min(max, Math.max(min, width)))
}

function storedWidth(value: unknown, limits: typeof NAVIGATION) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? clamp(value, limits.min, limits.max) : limits.initial
}

function loadWidths(): Widths {
  try {
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    return {navigation: storedWidth(stored?.navigation, NAVIGATION), list: storedWidth(stored?.list, LIST)}
  } catch {
    // Layout preferences are optional when browser storage is unavailable.
    return {navigation: NAVIGATION.initial, list: LIST.initial}
  }
}

function subscribeViewport(notify: () => void) {
  window.addEventListener('resize', notify)
  return () => window.removeEventListener('resize', notify)
}

export function usePaneWidths(narrow: boolean) {
  const viewport = useSyncExternalStore(subscribeViewport, () => window.innerWidth, () => 1280)
  const [preferred, setPreferred] = useState(loadWidths)
  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(preferred)) }
    catch { /* Resizing still works when the browser cannot save preferences. */ }
  }, [preferred])

  // Keep space for the list and reader. Window resizing only constrains the
  // displayed widths, so enlarging the window restores the user's preference.
  const navigationMax = Math.max(NAVIGATION.min, Math.min(NAVIGATION.max, viewport - LIST.min - READER_MIN))
  const navigationWidth = clamp(preferred.navigation, NAVIGATION.min, navigationMax)
  const listMax = Math.max(LIST.min, Math.min(LIST.max, viewport - (narrow ? 0 : navigationWidth) - READER_MIN))

  function pane(name: keyof Widths, limits: typeof NAVIGATION, max: number) {
    return {
      min: limits.min, max, value: clamp(preferred[name], limits.min, max),
      onChange: (width: number) => {
        if (Number.isFinite(width)) setPreferred(previous => ({...previous, [name]: clamp(width, limits.min, max)}))
      },
      onReset: () => setPreferred(previous => ({...previous, [name]: limits.initial})),
    }
  }

  return {navigation: pane('navigation', NAVIGATION, navigationMax), list: pane('list', LIST, listMax)}
}
