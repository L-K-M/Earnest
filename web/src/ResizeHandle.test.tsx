// @vitest-environment jsdom
import {useState} from 'react'
import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {ResizeHandle} from './ResizeHandle'

// jsdom does not implement pointer capture or PointerEvent yet.
class TestPointerEvent extends MouseEvent {
  pointerId: number
  isPrimary: boolean
  constructor(type: string, init: PointerEventInit = {}) {
    super(type, init)
    this.pointerId = init.pointerId ?? 1
    this.isPrimary = init.isPrimary ?? true
  }
}
beforeEach(() => vi.stubGlobal('PointerEvent', TestPointerEvent))
afterEach(() => {cleanup(); vi.unstubAllGlobals()})

function start() {
  const change = vi.fn()
  const reset = vi.fn()
  function Harness() {
    const [value, setValue] = useState(300)
    return <ResizeHandle label="Pane width" controls="pane" min={200} max={600} value={value}
      onChange={width => {change(width); setValue(width)}} onReset={() => {reset(); setValue(300)}} />
  }
  const view = render(<Harness />)
  const handle = screen.getByRole('separator', {name: 'Pane width'})
  let captured: number | null = null
  const capture = vi.fn((pointer: number) => {captured = pointer})
  const release = vi.fn(() => {captured = null})
  Object.assign(handle, {setPointerCapture: capture, hasPointerCapture: (pointer: number) => pointer === captured, releasePointerCapture: release})
  return {...view, handle, change, reset, capture, release}
}

it('captures the pointer and resizes by horizontal delta, constrained to its bounds', () => {
  const {handle, capture} = start()
  fireEvent.pointerDown(handle, {clientX: 300, button: 0})
  expect(capture).toHaveBeenCalledWith(1)
  expect(document.activeElement).toBe(handle)
  expect(document.querySelector('.resize-shield')).toBeTruthy()
  fireEvent.pointerMove(handle, {clientX: 400, clientY: 800})
  expect(handle.getAttribute('aria-valuenow')).toBe('400')
  fireEvent.pointerMove(handle, {clientX: 450})
  expect(handle.getAttribute('aria-valuenow')).toBe('450')
  fireEvent.pointerMove(handle, {clientX: 900})
  expect(handle.getAttribute('aria-valuenow')).toBe('600')
  fireEvent.pointerMove(handle, {clientX: -100})
  expect(handle.getAttribute('aria-valuenow')).toBe('200')
})

it.each(['pointerUp', 'pointerCancel', 'lostPointerCapture'] as const)('ends a drag on %s and removes its shield', event => {
  const {handle, release, change} = start()
  fireEvent.pointerDown(handle, {clientX: 300})
  fireEvent.pointerMove(handle, {clientX: 350})
  fireEvent[event](handle)
  expect(release).toHaveBeenCalledWith(1)
  expect(document.querySelector('.resize-shield')).toBeNull()
  fireEvent.pointerMove(handle, {clientX: 500})
  expect(change).toHaveBeenCalledTimes(1)
})

it('ignores secondary buttons, non-primary pointers and events from another pointer', () => {
  const {handle, capture, change} = start()
  fireEvent.pointerDown(handle, {button: 2})
  fireEvent.pointerDown(handle, {isPrimary: false})
  expect(capture).not.toHaveBeenCalled()
  fireEvent.pointerDown(handle, {clientX: 300})
  fireEvent.pointerDown(handle, {pointerId: 2, clientX: 100})
  fireEvent.pointerMove(handle, {pointerId: 2, clientX: 500})
  fireEvent.pointerUp(handle, {pointerId: 2})
  expect(change).not.toHaveBeenCalled()
  expect(document.querySelector('.resize-shield')).toBeTruthy()
  fireEvent.pointerMove(handle, {clientX: 350})
  expect(handle.getAttribute('aria-valuenow')).toBe('350')
})

it('releases capture and the overlay on blur or unmount', () => {
  const {handle, release, unmount} = start()
  fireEvent.pointerDown(handle, {clientX: 300})
  fireEvent(window, new Event('blur'))
  expect(document.querySelector('.resize-shield')).toBeNull()
  expect(release).toHaveBeenCalledTimes(1)
  fireEvent.pointerDown(handle, {clientX: 300})
  unmount()
  expect(release).toHaveBeenCalledTimes(2)
  expect(document.querySelector('.resize-shield')).toBeNull()
})

it('supports keyboard bounds, fine and larger steps, and resets without bubbling to article shortcuts', () => {
  const {handle, reset, change} = start()
  const parentKey = vi.fn()
  document.body.addEventListener('keydown', parentKey)
  try {
    expect(handle.getAttribute('aria-orientation')).toBe('vertical')
    expect(handle.getAttribute('aria-controls')).toBe('pane')
    expect(handle.getAttribute('aria-valuemin')).toBe('200')
    expect(handle.getAttribute('aria-valuemax')).toBe('600')
    fireEvent.keyDown(handle, {key: 'ArrowRight'})
    expect(handle.getAttribute('aria-valuetext')).toBe('316 pixels')
    fireEvent.keyDown(handle, {key: 'ArrowLeft', shiftKey: true})
    expect(handle.getAttribute('aria-valuenow')).toBe('268')
    fireEvent.keyDown(handle, {key: 'Home'})
    fireEvent.keyDown(handle, {key: 'ArrowLeft'})
    expect(handle.getAttribute('aria-valuenow')).toBe('200')
    fireEvent.keyDown(handle, {key: 'End'})
    fireEvent.keyDown(handle, {key: 'ArrowRight'})
    expect(handle.getAttribute('aria-valuenow')).toBe('600')
    fireEvent.keyDown(handle, {key: 'Enter'})
    expect(reset).toHaveBeenCalledOnce()
    expect(handle.getAttribute('aria-valuenow')).toBe('300')
    expect(parentKey).not.toHaveBeenCalled()
    const changes = change.mock.calls.length
    fireEvent.keyDown(handle, {key: 'ArrowRight', ctrlKey: true})
    fireEvent.keyDown(handle, {key: 'Tab'})
    expect(change).toHaveBeenCalledTimes(changes)
    expect(parentKey).toHaveBeenCalledTimes(2)
    fireEvent.doubleClick(handle)
    expect(reset).toHaveBeenCalledTimes(2)
  } finally { document.body.removeEventListener('keydown', parentKey) }
})
