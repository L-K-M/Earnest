import {useEffect, useRef, useState, type RefObject} from 'react'
import {shortcutsAllowed} from '../reader'
import './palette.css'

export type Command = {id: string; title: string; key: string; run: () => void | Promise<void>; disabled?: boolean}

// Rank contiguous matches first, then ordered subsequences, preserving ties.
export function fuzzyCommands(commands: Command[], query: string): Command[] {
  const needle = query.toLocaleLowerCase().trim()
  function score(title: string) {
    const haystack = title.toLocaleLowerCase()
    if (!needle) return 0
    const contiguous = haystack.indexOf(needle)
    if (contiguous >= 0) return contiguous
    let index = -1
    let cost = haystack.length
    for (const char of needle.replace(/\s/g,'')) {
      const next = haystack.indexOf(char,index+1)
      if (next < 0) return Infinity
      cost += next-index-1;index=next
    }
    return cost+1000
  }
  return commands.map((command,index) => ({command,index,score:score(command.title)})).filter(item => Number.isFinite(item.score)).sort((a,b) => a.score-b.score || a.index-b.index).map(item => item.command)
}

export const shortcutHelp = [['j / k','Next / previous article'], ['o / Enter','Open article'], ['v','Open original'], ['e','Done'], ['s','Toggle Later'], ['f','Toggle Keep'], ['m','Mark unread, clearing Done'], ['/','Search'], ['?','Keyboard help'], ['Ctrl+K / ⌘K','Command palette']] as const

export function Palette({commands,shortcuts,enabled=true,helpRef}: {commands: Command[]; shortcuts: boolean; enabled?: boolean; helpRef?: RefObject<HTMLDialogElement | null>}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const ownHelp = useRef<HTMLDialogElement>(null)
  const help = helpRef ?? ownHelp
  const input = useRef<HTMLInputElement>(null)
  const previousFocus = useRef<HTMLElement | null>(null)
  const [query,setQuery] = useState('')
  const [index,setIndex] = useState(0)
  const [error,setError] = useState('')
  const [running,setRunning] = useState(false)
  // The global shortcut listener is registered once; it reads the latest
  // commands through a ref so a new array identity does not re-bind it.
  const commandsRef = useRef(commands)
  useEffect(() => { commandsRef.current = commands }, [commands])
  const matches = fuzzyCommands(commands,query)
  const active = matches[Math.min(index,Math.max(0,matches.length-1))]
  function open(node: HTMLDialogElement | null) {
    if (!node || node.open) return
    previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setQuery('');setIndex(0);setError('');node.showModal()
    if (node === dialog.current) input.current?.focus()
  }
  function restoreFocus() {previousFocus.current?.focus()}
  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (!enabled || event.defaultPrevented || event.repeat || event.isComposing) return
      if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === 'k') {event.preventDefault();open(dialog.current);return}
      if (event.ctrlKey || event.metaKey || event.altKey || !shortcutsAllowed(event.target,shortcuts)) return
      if (event.key === '?') {event.preventDefault();open(help.current);return}
      if (event.key === '/') {
        const search = commandsRef.current.find(command => command.id === 'search')
        if (search) {event.preventDefault();void search.run()}
      }
    }
    window.addEventListener('keydown',keyboard)
    return () => window.removeEventListener('keydown',keyboard)
  },[enabled,shortcuts,help])
  async function run(command: Command | undefined) {
    if (!command || command.disabled || running) return
    setRunning(true);setError('')
    try {await command.run();dialog.current?.close();restoreFocus()}
    catch(error) {setError(error instanceof Error ? error.message : 'Command failed.')}
    finally {setRunning(false)}
  }
  return <>
    <button onClick={() => open(dialog.current)}>Command palette (Ctrl+K / ⌘K)</button><button onClick={() => open(help.current)}>Keyboard help (?)</button>
    <dialog ref={dialog} aria-labelledby="palette-title" onClose={restoreFocus}><h2 id="palette-title">Command palette</h2>
      <label>Find a command<input ref={input} role="combobox" aria-autocomplete="list" aria-expanded="true" aria-controls="palette-results" aria-activedescendant={active ? `command-${active.id}` : undefined} value={query} onChange={event => {setQuery(event.target.value);setIndex(0)}} onKeyDown={event => {
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {event.preventDefault();setIndex(old => Math.max(0,Math.min(matches.length-1,old+(event.key === 'ArrowDown' ? 1 : -1))))}
        if (event.key === 'Enter') {event.preventDefault();void run(active)}
      }}/></label>
      <ul id="palette-results" role="listbox" aria-label="Commands">{matches.map(command => <li id={`command-${command.id}`} key={command.id} role="option" aria-selected={active?.id === command.id} aria-disabled={command.disabled || running}><button disabled={command.disabled || running} onClick={() => {void run(command)}}>{command.title} <kbd>{command.key}</kbd></button></li>)}</ul>
      {!matches.length && <p>No matching commands.</p>}{error && <p role="alert">{error}</p>}<button onClick={() => dialog.current?.close()}>Close palette</button>
    </dialog>
    <dialog ref={help} aria-labelledby="shortcuts-title" onClose={restoreFocus}><h2 id="shortcuts-title">Keyboard shortcuts</h2><p>Single-key shortcuts work outside text fields. You can switch them off in Settings. Ctrl+K and ⌘K always open the palette.</p><dl>{shortcutHelp.map(([key,title]) => <div key={key}><dt><kbd>{key}</kbd></dt><dd>{title}</dd></div>)}</dl><button onClick={() => help.current?.close()}>Close help</button></dialog>
  </>
}
