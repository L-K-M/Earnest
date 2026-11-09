import {useEffect, useId, useImperativeHandle, useRef, useState, type Ref, type RefObject} from 'react'
import {shortcutsAllowed} from '../reader'
import './palette.css'

export type Command = {id: string; title: string; key?: string; group?: string; run: () => void | Promise<void>; disabled?: boolean}
export type PaletteHandle = {openPalette: () => void; openHelp: () => void}

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

export const shortcutGroups: readonly {title: string; keys: readonly (readonly [string, string])[]}[] = [
  {title:'Moving around', keys:[['j / k','Next or previous article; while reading, open it'], ['↓ / ↑','Next or previous article in the list'], ['o / Enter','Open article'], ['Esc / u','Close the article'], ['g then t, e, l, k, d','Today, Edition, Later, Kept, Done'], ['g then f, s','Manage feeds, Settings'], ['g then a, n, r','Activity, Notifications, Rules']]},
  {title:'Article', keys:[['e','Done'], ['s','Toggle Later'], ['f','Toggle Keep'], ['m','Mark unread, clearing Done'], ['z','Undo the last action'], ['v','Open original']]},
  {title:'Everywhere', keys:[['/','Search'], ['Ctrl+K / ⌘K','Command palette'], ['Shift+F10','Actions for the focused article, feed or folder'], ['?','Keyboard shortcuts']]},
]
export const shortcutHelp = shortcutGroups.flatMap(group => group.keys)

export function Palette({commands,shortcuts,enabled=true,helpRef,triggers=true,ref}: {commands: Command[]; shortcuts: boolean; enabled?: boolean; helpRef?: RefObject<HTMLDialogElement | null>; triggers?: boolean; ref?: Ref<PaletteHandle>}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const ownHelp = useRef<HTMLDialogElement>(null)
  const help = helpRef ?? ownHelp
  const input = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const previousFocus = useRef(new Map<HTMLDialogElement, HTMLElement | null>())
  const ids = useId()
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
  const optionID = (command: Command) => `${ids}-command-${command.id}`
  function open(node: HTMLDialogElement | null) {
    if (!node || node.open) return
    // Each dialog remembers its own opener, so stacked dialogs restore focus
    // to the right place instead of overwriting a single shared slot.
    previousFocus.current.set(node, document.activeElement instanceof HTMLElement ? document.activeElement : null)
    setQuery('');setIndex(0);setError('');node.showModal()
    if (node === dialog.current) input.current?.focus()
  }
  function restoreFocus(node: HTMLDialogElement | null) {
    const target = node ? previousFocus.current.get(node) ?? null : null
    if (node) previousFocus.current.delete(node)
    if (target?.isConnected) target.focus()
  }
  useImperativeHandle(ref, () => ({openPalette:() => open(dialog.current), openHelp:() => open(help.current)}))
  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (!enabled || event.defaultPrevented || event.repeat || event.isComposing) return
      if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === 'k') {event.preventDefault();open(dialog.current);return}
      // Focus sits inside the open help dialog, where single keys are
      // otherwise ignored; a second ? still closes it.
      if (event.key === '?' && help.current?.open && event.target instanceof Node && help.current.contains(event.target)) {event.preventDefault();help.current.close();return}
      if (event.ctrlKey || event.metaKey || event.altKey || !shortcutsAllowed(event.target,shortcuts)) return
      if (event.key === '?') {
        // Toggle: a second press closes the dialog instead of calling
        // showModal() on an open element, which throws InvalidStateError.
        event.preventDefault()
        const node = help.current
        if (node) { if (node.open) node.close(); else open(node) }
        return
      }
      if (event.key === '/') {
        const search = commandsRef.current.find(command => command.id === 'search')
        if (search) {event.preventDefault();void search.run()}
      }
    }
    window.addEventListener('keydown',keyboard)
    return () => window.removeEventListener('keydown',keyboard)
  },[enabled,shortcuts,help])
  // Keep the active option visible while arrowing through a long list.
  useEffect(() => {
    if (!active) return
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({block:'nearest'})
  })
  async function run(command: Command | undefined) {
    if (!command || command.disabled || running) return
    setRunning(true);setError('')
    try {await command.run();dialog.current?.close();restoreFocus(dialog.current)}
    catch(error) {setError(error instanceof Error ? error.message : 'Command failed.')}
    finally {setRunning(false)}
  }
  function move(delta: number) {
    setIndex(old => matches.length ? (Math.min(old, matches.length - 1) + delta + matches.length) % matches.length : 0)
  }
  return <>
    {triggers && <><button type="button" onClick={() => open(dialog.current)}>Command palette (Ctrl+K / ⌘K)</button><button type="button" onClick={() => open(help.current)}>Keyboard help (?)</button></>}
    <dialog ref={dialog} className="palette" aria-labelledby={`${ids}-palette-title`} onClose={() => restoreFocus(dialog.current)} onClick={event => {if (event.target === dialog.current) dialog.current.close()}}>
      <h2 id={`${ids}-palette-title`} className="sr-only">Command palette</h2>
      <div className="palette-search">
        <label htmlFor={`${ids}-palette-input`} className="sr-only">Find a command</label>
        <input id={`${ids}-palette-input`} ref={input} type="text" role="combobox" autoComplete="off" spellCheck={false} placeholder="Go to a view, feed or action…" aria-autocomplete="list" aria-expanded="true" aria-controls={`${ids}-palette-results`} aria-activedescendant={active ? optionID(active) : undefined} value={query} onChange={event => {setQuery(event.target.value);setIndex(0)}} onKeyDown={event => {
          // While an input method is composing, Enter and the arrows belong to it.
          if (event.nativeEvent.isComposing) return
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {event.preventDefault();move(event.key === 'ArrowDown' ? 1 : -1)}
          if (event.key === 'Home' && event.ctrlKey) {event.preventDefault();setIndex(0)}
          if (event.key === 'End' && event.ctrlKey) {event.preventDefault();setIndex(Math.max(0, matches.length - 1))}
          if (event.key === 'Enter') {event.preventDefault();void run(active)}
        }}/>
        <kbd className="palette-esc" aria-hidden="true">Esc</kbd>
      </div>
      <ul ref={list} id={`${ids}-palette-results`} role="listbox" aria-label="Commands" className="palette-results">{matches.map(command => <li id={optionID(command)} key={command.id} role="option" aria-selected={active?.id === command.id} aria-disabled={command.disabled || running ? true : undefined} onPointerMove={() => {const at = matches.indexOf(command); if (at !== index) setIndex(at)}} onClick={() => {void run(command)}}>
        <span className="palette-title">{command.title}</span>{command.group && <> <span className="palette-group">{command.group}</span></>}{command.key && <> <kbd>{command.key}</kbd></>}
      </li>)}</ul>
      {!matches.length && <p className="palette-empty">No matching commands. Try a feed name, “Later” or “Settings”.</p>}
      {error && <p role="alert" className="palette-error">{error}</p>}
      <p className="palette-hint" aria-hidden="true"><kbd>↑</kbd><kbd>↓</kbd> to move · <kbd>Enter</kbd> to run · <kbd>Esc</kbd> to close</p>
    </dialog>
    <dialog ref={help} className="shortcuts" aria-labelledby={`${ids}-shortcuts-title`} onClose={() => restoreFocus(help.current)} onClick={event => {if (event.target === help.current) help.current.close()}}>
      <div className="dialog-header"><h2 id={`${ids}-shortcuts-title`}>Keyboard shortcuts</h2><button type="button" className="quiet" onClick={() => help.current?.close()}>Close help</button></div>
      <p className="muted">Single-key shortcuts work outside text fields. {shortcuts ? 'You can switch them off in Settings.' : 'They are switched off in Settings.'} Ctrl+K and ⌘K always open the palette.</p>
      <div className="shortcut-groups">{shortcutGroups.map((group,at) => <section key={group.title} aria-labelledby={`${ids}-group-${at}`}><h3 id={`${ids}-group-${at}`}>{group.title}</h3><dl>{group.keys.map(([key,title]) => <div key={key}><dt><kbd>{key}</kbd></dt><dd>{title}</dd></div>)}</dl></section>)}</div>
    </dialog>
  </>
}
