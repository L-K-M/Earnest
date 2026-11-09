// @vitest-environment jsdom
import {afterEach, describe, expect, it, vi} from 'vitest'
import {act, cleanup, render} from '@testing-library/react'
import {ArticleFrame, frameMessage, scrollMove} from './ArticleFrame'

afterEach(() => { cleanup(); vi.restoreAllMocks() })

describe('scrollMove', () => {
  it('pages like the browser and steps by lines', () => {
    expect(scrollMove(' ', false, 800)).toEqual({by: 700})
    expect(scrollMove(' ', true, 800)).toEqual({by: -700})
    expect(scrollMove('PageDown', false, 800)).toEqual({by: 700})
    expect(scrollMove('PageUp', false, 800)).toEqual({by: -700})
    expect(scrollMove('ArrowDown', false, 800)).toEqual({by: 40})
    expect(scrollMove('ArrowUp', false, 800)).toEqual({by: -40})
    expect(scrollMove('Home', false, 800)).toEqual({to: 'top'})
    expect(scrollMove('End', false, 800)).toEqual({to: 'bottom'})
    expect(scrollMove('j', false, 800)).toBeNull()
  })
  it('still moves in a tiny pane', () => {
    expect(scrollMove(' ', false, 10)).toEqual({by: 40})
  })
})

describe('frameMessage', () => {
  it('accepts heights and keys, clamping the height', () => {
    expect(frameMessage({earnest: 'height', height: 1234.2})).toEqual({earnest: 'height', height: 1235})
    expect(frameMessage({earnest: 'height', height: 5e9})).toEqual({earnest: 'height', height: 1_000_000})
    expect(frameMessage({earnest: 'key', key: 'j', shift: false})).toEqual({earnest: 'key', key: 'j', shift: false})
    expect(frameMessage({earnest: 'key', key: ' ', shift: 'yes'})).toEqual({earnest: 'key', key: ' ', shift: false})
  })
  it('ignores anything else', () => {
    for (const data of [null, 'height', 4, {earnest: 'height', height: -1}, {earnest: 'height', height: Number.NaN}, {earnest: 'height'}, {earnest: 'key', key: ''}, {earnest: 'key', key: 'x'.repeat(17)}, {earnest: 'other'}]) {
      expect(frameMessage(data)).toBeNull()
    }
  })
})

function mount() {
  const view = render(<div className="reader-scroll"><ArticleFrame src="https://content.test/content/1" /></div>)
  const pane = view.container.querySelector<HTMLDivElement>('.reader-scroll')!
  const frame = view.container.querySelector('iframe')!
  return {pane, frame}
}
function post(source: MessageEventSource | null, data: unknown) {
  act(() => { window.dispatchEvent(new MessageEvent('message', {data, source})) })
}

describe('ArticleFrame', () => {
  it('runs the article page script in an opaque-origin sandbox', () => {
    const {frame} = mount()
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-popups allow-popups-to-escape-sandbox')
    expect(frame.title).toBe('Article body')
  })

  it('sizes itself to the height its own page reports', () => {
    const {frame} = mount()
    expect(frame.style.height).toBe('')
    post(frame.contentWindow, {earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('2400px')
  })

  it('ignores messages from any other window', () => {
    const {frame} = mount()
    post(window, {earnest: 'height', height: 2400})
    post(null, {earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('')
  })

  it('scrolls the reading pane for scrolling keys pressed in the article', () => {
    const {frame, pane} = mount()
    Object.defineProperty(pane, 'clientHeight', {value: 800})
    Object.defineProperty(pane, 'scrollHeight', {value: 5000})
    const scrollBy = vi.fn()
    const scrollTo = vi.fn()
    pane.scrollBy = scrollBy as typeof pane.scrollBy
    pane.scrollTo = scrollTo as typeof pane.scrollTo
    const keys = vi.fn()
    window.addEventListener('keydown', keys)
    post(frame.contentWindow, {earnest: 'key', key: ' ', shift: false})
    post(frame.contentWindow, {earnest: 'key', key: ' ', shift: true})
    post(frame.contentWindow, {earnest: 'key', key: 'End', shift: false})
    window.removeEventListener('keydown', keys)
    expect(scrollBy.mock.calls).toEqual([[{top: 700}], [{top: -700}]])
    expect(scrollTo).toHaveBeenCalledWith({top: 5000})
    expect(keys).not.toHaveBeenCalled()
  })

  it('hands other keys to the app as if pressed beside the frame', () => {
    const {frame} = mount()
    const keys = vi.fn((event: KeyboardEvent) => [event.key, event.shiftKey, event.target])
    window.addEventListener('keydown', keys)
    post(frame.contentWindow, {earnest: 'key', key: 'j', shift: false})
    post(frame.contentWindow, {earnest: 'key', key: 'Escape', shift: false})
    window.removeEventListener('keydown', keys)
    expect(keys.mock.results.map(result => result.value)).toEqual([['j', false, frame], ['Escape', false, frame]])
  })
})
