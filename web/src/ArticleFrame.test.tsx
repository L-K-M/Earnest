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
  it('accepts heights and keys carrying the load nonce, clamping the height', () => {
    expect(frameMessage({nonce: 'n', earnest: 'height', height: 1234.2}, 'n')).toEqual({earnest: 'height', height: 1235})
    expect(frameMessage({nonce: 'n', earnest: 'height', height: 5e9}, 'n')).toEqual({earnest: 'height', height: 1_000_000})
    expect(frameMessage({nonce: 'n', earnest: 'key', key: 'j', shift: false}, 'n')).toEqual({earnest: 'key', key: 'j', shift: false})
    expect(frameMessage({nonce: 'n', earnest: 'key', key: ' ', shift: 'yes'}, 'n')).toEqual({earnest: 'key', key: ' ', shift: false})
    expect(frameMessage({nonce: 'n', earnest: 'key', key: 'Escape', shift: false}, 'n')).toEqual({earnest: 'key', key: 'Escape', shift: false})
    expect(frameMessage({nonce: 'n', earnest: 'key', key: 'PageDown', shift: false}, 'n')).toEqual({earnest: 'key', key: 'PageDown', shift: false})
  })
  it('ignores anything else', () => {
    for (const data of [null, 'height', 4, {earnest: 'height', height: -1}, {earnest: 'height', height: Number.NaN}, {earnest: 'height'}, {earnest: 'other'}, {nonce: 'n', earnest: 'key', key: ''}, {nonce: 'n', earnest: 'key', key: 'x'.repeat(17)}, {nonce: 'n', earnest: 'key', key: 'Enter'}, {nonce: 'n', earnest: 'key', key: 'F5'}]) {
      expect(frameMessage(data, 'n')).toBeNull()
    }
  })
  it('ignores messages that do not carry this load nonce', () => {
    expect(frameMessage({nonce: 'other', earnest: 'height', height: 2400}, 'n')).toBeNull()
    expect(frameMessage({earnest: 'height', height: 2400}, 'n')).toBeNull()
    expect(frameMessage({nonce: 'n', earnest: 'height', height: 2400}, '')).toBeNull()
  })
})

function mount(src = 'https://content.test/content/1') {
  const view = render(<div className="reader-scroll"><ArticleFrame src={src} /></div>)
  const pane = view.container.querySelector<HTMLDivElement>('.reader-scroll')!
  const frame = view.container.querySelector('iframe')!
  const nonce = new URL(frame.src).searchParams.get('frame_nonce')!
  return {view, pane, frame, nonce}
}
function post(source: MessageEventSource | null, data: unknown) {
  act(() => { window.dispatchEvent(new MessageEvent('message', {data, source})) })
}

describe('ArticleFrame', () => {
  it('runs the article page script in an opaque-origin sandbox', () => {
    const {frame, nonce} = mount()
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-popups allow-popups-to-escape-sandbox')
    expect(frame.title).toBe('Article body')
    expect(nonce).toBeTruthy()
    expect(frame.src).toContain(`frame_nonce=${encodeURIComponent(nonce)}`)
  })

  it('sizes itself to the height its own page reports', () => {
    const {frame, nonce} = mount()
    expect(frame.style.height).toBe('')
    post(frame.contentWindow, {nonce, earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('2400px')
  })

  it('ignores messages from any other window', () => {
    const {frame, nonce} = mount()
    post(window, {nonce, earnest: 'height', height: 2400})
    post(null, {nonce, earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('')
  })

  it('ignores messages that do not carry this load nonce', () => {
    const {frame} = mount()
    post(frame.contentWindow, {nonce: 'stale', earnest: 'height', height: 2400})
    post(frame.contentWindow, {earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('')
  })

  it('starts the next article unsized and drops the previous nonce', () => {
    const {view, frame, nonce} = mount()
    post(frame.contentWindow, {nonce, earnest: 'height', height: 2400})
    expect(frame.style.height).toBe('2400px')
    view.rerender(<div className="reader-scroll"><ArticleFrame src="https://content.test/content/2" /></div>)
    const next = view.container.querySelector('iframe')!
    expect(next.style.height).toBe('')
    const second = new URL(next.src).searchParams.get('frame_nonce')!
    expect(second).not.toBe(nonce)
    post(next.contentWindow, {nonce, earnest: 'height', height: 2400})
    expect(next.style.height).toBe('')
    post(next.contentWindow, {nonce: second, earnest: 'height', height: 900})
    expect(next.style.height).toBe('900px')
  })

  it('scrolls the reading pane for scrolling keys pressed in the article', () => {
    const {frame, pane, nonce} = mount()
    Object.defineProperty(pane, 'clientHeight', {value: 800})
    Object.defineProperty(pane, 'scrollHeight', {value: 5000})
    const scrollBy = vi.fn()
    const scrollTo = vi.fn()
    pane.scrollBy = scrollBy as typeof pane.scrollBy
    pane.scrollTo = scrollTo as typeof pane.scrollTo
    const keys = vi.fn()
    window.addEventListener('keydown', keys)
    post(frame.contentWindow, {nonce, earnest: 'key', key: ' ', shift: false})
    post(frame.contentWindow, {nonce, earnest: 'key', key: ' ', shift: true})
    post(frame.contentWindow, {nonce, earnest: 'key', key: 'End', shift: false})
    window.removeEventListener('keydown', keys)
    expect(scrollBy.mock.calls).toEqual([[{top: 700}], [{top: -700}]])
    expect(scrollTo).toHaveBeenCalledWith({top: 5000})
    expect(keys).not.toHaveBeenCalled()
  })

  it('hands other keys to the app as if pressed beside the frame', () => {
    const {frame, nonce} = mount()
    const keys = vi.fn((event: KeyboardEvent) => [event.key, event.shiftKey, event.target])
    window.addEventListener('keydown', keys)
    post(frame.contentWindow, {nonce, earnest: 'key', key: 'j', shift: false})
    post(frame.contentWindow, {nonce, earnest: 'key', key: 'Escape', shift: false})
    window.removeEventListener('keydown', keys)
    expect(keys.mock.results.map(result => result.value)).toEqual([['j', false, frame], ['Escape', false, frame]])
  })
})
