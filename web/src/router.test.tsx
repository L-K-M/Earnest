// @vitest-environment jsdom
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {useRef} from 'react'
import {act, cleanup, fireEvent, render, screen} from '@testing-library/react'
import {initializeHistory, Link, navigate, openedFrom, returnTo, useBlocker, useLocation, useScrollRestoration} from './router'

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.href} {location.action}</p>
}
function Guarded({active}: {active: boolean}) {
  const release = useBlocker(active, 'Discard changes?')
  return <button onClick={release}>Release</button>
}
function ScrollBox() {
  const location = useLocation()
  const ref = useRef<HTMLDivElement>(null)
  useScrollRestoration(ref, location, 'test')
  return <div ref={ref} data-testid="scroll" />
}
async function frames(count = 2) {
  await act(async () => { for (let i = 0; i < count; i++) await new Promise(resolve => requestAnimationFrame(() => resolve(null))) })
}
// history.go is asynchronous; wait for the popstate it produces.
async function popTo(steps: number) {
  await act(async () => {
    const popped = new Promise(resolve => window.addEventListener('popstate', resolve, {once: true}))
    history.go(steps)
    await popped
  })
}
beforeEach(() => {history.replaceState(null, '', '/');initializeHistory()})
afterEach(() => {cleanup();vi.restoreAllMocks()})

it('pushes, replaces and reports the kind of navigation', async () => {
  render(<Where />)
  act(() => {navigate('/feeds/3')})
  expect(screen.getByTestId('where').textContent).toBe('/feeds/3 push')
  act(() => {navigate('/feeds/3/articles/9', {replace: true})})
  expect(screen.getByTestId('where').textContent).toBe('/feeds/3/articles/9 replace')
  await popTo(-1)
  expect(screen.getByTestId('where').textContent).toBe('/ pop')
})

it('navigates in place on a plain click and leaves modified clicks to the browser', () => {
  render(<><Link to="/later">Later</Link><Where /></>)
  const link = screen.getByRole('link', {name: 'Later'})
  expect(link.getAttribute('href')).toBe('/later')
  const modified = new MouseEvent('click', {bubbles: true, cancelable: true, metaKey: true})
  link.dispatchEvent(modified)
  expect(location.pathname).toBe('/')
  fireEvent.click(link)
  expect(location.pathname).toBe('/later')
})

it('returns to the list an article was opened from instead of stacking another entry', async () => {
  render(<Where />)
  act(() => {navigate('/today')})
  act(() => {navigate('/today/articles/4', {from: '/today'})})
  expect(openedFrom()).toBe('/today')
  const length = history.length
  await act(async () => {
    const popped = new Promise(resolve => window.addEventListener('popstate', resolve, {once: true}))
    returnTo('/today')
    await popped
  })
  expect(location.pathname).toBe('/today')
  expect(history.length).toBe(length)
})

it('asks before leaving unsaved work and stops asking once it is released', () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  render(<><Guarded active /><Where /></>)
  act(() => {navigate('/settings')})
  expect(confirm).toHaveBeenCalledWith('Discard changes?')
  expect(location.pathname).toBe('/')
  fireEvent.click(screen.getByRole('button', {name: 'Release'}))
  act(() => {navigate('/settings')})
  expect(confirm).toHaveBeenCalledTimes(1)
  expect(location.pathname).toBe('/settings')
})

it('undoes a Back the reader declines to take', async () => {
  render(<Where />)
  act(() => {navigate('/rules')})
  act(() => {navigate('/rules/new')})
  const {rerender} = render(<Guarded active />)
  vi.spyOn(window, 'confirm').mockReturnValue(false)
  await popTo(-1)
  await act(async () => {await new Promise(resolve => window.setTimeout(resolve, 20))})
  expect(location.pathname).toBe('/rules/new')
  rerender(<Guarded active={false} />)
})

it('restores a saved scroll position of zero', async () => {
  render(<ScrollBox />)
  const node = screen.getByTestId('scroll')
  const scrollTo = vi.fn()
  Object.defineProperty(node, 'scrollTo', {value: scrollTo, writable: true})
  act(() => { navigate('/a') })
  act(() => { node.scrollTop = 0; node.dispatchEvent(new Event('scroll')) })
  await frames()
  act(() => { navigate('/b') })
  act(() => { node.scrollTop = 800; node.dispatchEvent(new Event('scroll')) })
  await frames()
  scrollTo.mockClear()
  await popTo(-1)
  await frames(3)
  expect(scrollTo).toHaveBeenCalledWith({top: 0})
})

it('refuses to navigate anywhere but http and https', () => {
  const assign = vi.fn()
  vi.spyOn(window, 'location', 'get').mockReturnValue({...window.location, assign, href: window.location.href, origin: window.location.origin} as Location)
  expect(navigate('javascript:alert(document.domain)')).toBe(false)
  expect(navigate('data:text/html,<script>alert(1)</script>')).toBe(false)
  expect(assign).not.toHaveBeenCalled()
})
