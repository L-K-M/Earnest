// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {fuzzyCommands,Palette,type Command} from './Palette'

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function() {this.setAttribute('open','')}
  HTMLDialogElement.prototype.close = function() {this.removeAttribute('open');this.dispatchEvent(new Event('close'))}
})
afterEach(() => {cleanup();vi.resetAllMocks()})
function commands(): Command[] {return ['Today','Feed: Rust','Label: Research','Search: Research','Mark Done','Toggle Later','Toggle Keep','Mark unread','Subscribe','Search','Settings'].map((title,i) => ({id:i === 9 ? 'search' : String(i),title,key:i === 4 ? 'e' : 'Ctrl+K, Enter',run:vi.fn()}))}
it('fuzzy matches every resource and action without changing stable tie order',() => {
  const all = commands()
  expect(fuzzyCommands(all,'rsrch').map(c => c.title)).toEqual(['Label: Research','Search: Research'])
  expect(fuzzyCommands(all,'tglk').map(c => c.title)).toEqual(['Toggle Keep'])
  expect(fuzzyCommands(all,'not present')).toEqual([])
  expect(fuzzyCommands(all,'')).toEqual(all)
})
it.each([{ctrlKey:true},{metaKey:true}])('prevents the browser shortcut and opens the palette with %j',modifiers => {
  render(<Palette commands={commands()} shortcuts={false}/>)
  const target = screen.getByRole('button',{name:/Command palette/})
  target.focus()
  const event = new KeyboardEvent('keydown',{key:'k',...modifiers,bubbles:true,cancelable:true})
  target.dispatchEvent(event)
  expect(event.defaultPrevented).toBe(true)
  expect(screen.getByRole('dialog',{name:'Command palette'})).toBeTruthy()
  expect(document.activeElement).toBe(screen.getByRole('combobox'))
})
it('runs the selected filtered command with Enter and restores focus',async () => {
  const all = commands()
  render(<Palette commands={all} shortcuts/> )
  const trigger = screen.getByRole('button',{name:/Command palette/})
  trigger.focus();fireEvent.click(trigger)
  fireEvent.change(screen.getByRole('combobox'),{target:{value:'tgl'}})
  fireEvent.keyDown(screen.getByRole('combobox'),{key:'ArrowDown'})
  fireEvent.keyDown(screen.getByRole('combobox'),{key:'Enter'})
  await waitFor(() => expect(all.find(c => c.title === 'Toggle Later')!.run).toHaveBeenCalledOnce())
  expect(screen.queryByRole('dialog',{name:'Command palette'})).toBeNull()
  expect(document.activeElement).toBe(trigger)
})
it('respects single-key opt-out and editable fields while leaving modified K available',() => {
  const all = commands()
  const {rerender} = render(<><input aria-label="Typing"/><Palette commands={all} shortcuts={false}/></>)
  fireEvent.keyDown(document.body,{key:'?'})
  expect(screen.queryByRole('dialog')).toBeNull()
  rerender(<><input aria-label="Typing"/><Palette commands={all} shortcuts/></>)
  fireEvent.keyDown(screen.getByLabelText('Typing'),{key:'?'})
  expect(screen.queryByRole('dialog')).toBeNull()
  fireEvent.keyDown(document.body,{key:'?'})
  expect(screen.getByRole('dialog',{name:'Keyboard shortcuts'})).toBeTruthy()
  // A second press toggles the dialog closed instead of calling showModal()
  // again, which would throw InvalidStateError in a browser.
  fireEvent.keyDown(document.body,{key:'?'})
  expect(screen.queryByRole('dialog',{name:'Keyboard shortcuts'})).toBeNull()
  fireEvent.keyDown(document.body,{key:'?'})
  expect(screen.getByRole('dialog',{name:'Keyboard shortcuts'})).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Close help'}))
  fireEvent.keyDown(document.body,{key:'/'})
  expect(all.find(c => c.id === 'search')!.run).toHaveBeenCalledOnce()
})
it('shows command errors and does not run disabled actions',async () => {
  const run = vi.fn().mockRejectedValue(new Error('Reading state is not ready.'))
  render(<Palette commands={[{id:'done',title:'Done',key:'e',run},{id:'keep',title:'Keep',key:'f',run,disabled:true}]} shortcuts/> )
  fireEvent.click(screen.getByRole('button',{name:/Command palette/}))
  expect(screen.getByRole('option',{name:'Keep f'}).getAttribute('aria-disabled')).toBe('true')
  fireEvent.click(screen.getByRole('option',{name:'Keep f'}))
  await new Promise(resolve => setTimeout(resolve, 0))
  expect(run).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('option',{name:'Done e'}))
  expect(await screen.findByRole('alert')).toHaveProperty('textContent','Reading state is not ready.')
  expect(screen.getByRole('dialog',{name:'Command palette'})).toBeTruthy()
})

it('leaves Enter and arrows to an input method while it composes',async () => {
  const all = commands()
  render(<Palette commands={all} shortcuts/>)
  fireEvent.click(screen.getByRole('button',{name:/Command palette/}))
  fireEvent.change(screen.getByRole('combobox'),{target:{value:'today'}})
  fireEvent.keyDown(screen.getByRole('combobox'),{key:'Enter',isComposing:true})
  await new Promise(resolve => setTimeout(resolve, 0))
  expect(all[0].run).not.toHaveBeenCalled()
  expect(screen.getByRole('dialog',{name:'Command palette'})).toBeTruthy()
})
it('closes the shortcut help with a second ? pressed inside it',() => {
  render(<Palette commands={commands()} shortcuts/>)
  fireEvent.keyDown(document.body,{key:'?'})
  const close = screen.getByRole('button',{name:'Close help'})
  close.focus()
  fireEvent.keyDown(close,{key:'?'})
  expect(screen.queryByRole('dialog',{name:'Keyboard shortcuts'})).toBeNull()
})
