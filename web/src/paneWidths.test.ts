// @vitest-environment jsdom
import {act, cleanup, renderHook} from '@testing-library/react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {usePaneWidths} from './paneWidths'

const key = 'earnest.pane-widths'
function viewport(width: number) {
  Object.defineProperty(window, 'innerWidth', {value: width, configurable: true})
  window.dispatchEvent(new Event('resize'))
}
const originalWidth = window.innerWidth
beforeEach(() => viewport(1600))
afterEach(() => {cleanup(); vi.restoreAllMocks(); localStorage.clear(); viewport(originalWidth)})

it('starts at the existing widths and saves each pane independently across remounts', () => {
  const {result, unmount} = renderHook(() => usePaneWidths(false))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([248, 432])
  act(() => result.current.navigation.onChange(320))
  act(() => result.current.list.onChange(560))
  expect(JSON.parse(localStorage.getItem(key)!)).toEqual({navigation: 320, list: 560})
  unmount()
  const restored = renderHook(() => usePaneWidths(false)).result
  expect([restored.current.navigation.value, restored.current.list.value]).toEqual([320, 560])
  act(() => restored.current.navigation.onReset())
  expect([restored.current.navigation.value, restored.current.list.value]).toEqual([248, 560])
  act(() => restored.current.list.onReset())
  expect(restored.current.list.value).toBe(432)
})

it('reserves reader space without overwriting preferred widths when the window shrinks', () => {
  localStorage.setItem(key, JSON.stringify({navigation: 480, list: 720}))
  const {result, rerender} = renderHook(({narrow}) => usePaneWidths(narrow), {initialProps: {narrow: false}})
  act(() => viewport(1100))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([476, 304])
  expect(1100 - result.current.navigation.value - result.current.list.value).toBe(320)
  expect(JSON.parse(localStorage.getItem(key)!)).toEqual({navigation: 480, list: 720})
  act(() => viewport(760))
  rerender({narrow: true})
  expect(result.current.list.value).toBe(440)
  act(() => viewport(1600))
  rerender({narrow: false})
  expect([result.current.navigation.value, result.current.list.value]).toEqual([480, 720])
})

it('clamps user changes to the available space and ignores non-finite widths', () => {
  const {result} = renderHook(() => usePaneWidths(false))
  act(() => result.current.navigation.onChange(-100))
  act(() => result.current.list.onChange(10000))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([200, 720])
  act(() => result.current.list.onChange(NaN))
  expect(result.current.list.value).toBe(720)
  act(() => viewport(1100))
  act(() => result.current.navigation.onChange(10000))
  act(() => result.current.list.onChange(10000))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([476, 304])
})

it.each(['bad json', 'null', '[]', '{"navigation":"320","list":-20}', '{"navigation":1e400}'])('ignores invalid saved values: %s', stored => {
  localStorage.setItem(key, stored)
  const {result} = renderHook(() => usePaneWidths(false))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([248, 432])
})

it('clamps valid saved numbers to the supported bounds', () => {
  localStorage.setItem(key, '{"navigation":10000,"list":1}')
  const {result} = renderHook(() => usePaneWidths(false))
  expect([result.current.navigation.value, result.current.list.value]).toEqual([480, 304])
})

it('keeps resizing usable when storage cannot be read or written', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {throw new Error('Unavailable')})
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {throw new Error('Full')})
  const {result} = renderHook(() => usePaneWidths(false))
  act(() => result.current.navigation.onChange(300))
  expect(result.current.navigation.value).toBe(300)
})
