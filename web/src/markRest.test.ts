import {describe, expect, it, vi} from 'vitest'
import {markRestCommand} from './markRest'
import type {Subscription} from './reader'

const subs = [{id:1, cadence:'today'}, {id:2, cadence:'week'}] as unknown as Subscription[]

describe('markRestCommand', () => {
  it('marks a feed read through the newest loaded entry after confirmation', async () => {
    const markRange = vi.fn().mockResolvedValue(undefined)
    const command = markRestCommand({kind:'feed', id:7, title:'Feed'}, subs, 42, markRange, () => true)
    expect(command.disabled).toBe(false)
    await command.run()
    expect(markRange).toHaveBeenCalledWith({kind:'subscriptions', ids:[7]}, 42)
  })

  it('leaves everything unread when the confirmation is declined', async () => {
    const markRange = vi.fn()
    await markRestCommand({kind:'folder', id:3, title:'Folder'}, subs, 9, markRange, () => false).run()
    expect(markRange).not.toHaveBeenCalled()
  })

  it('scopes Today to the feeds it shows', async () => {
    const markRange = vi.fn().mockResolvedValue(undefined)
    await markRestCommand({kind:'today', title:'Today'}, subs, 5, markRange, () => true).run()
    expect(markRange).toHaveBeenCalledWith({kind:'subscriptions', ids:[1]}, 5)
  })

  it('is unavailable before anything loads and in views without a range', () => {
    expect(markRestCommand({kind:'feed', id:7, title:'Feed'}, subs, 0, vi.fn(), () => true).disabled).toBe(true)
    expect(markRestCommand({kind:'later', title:'Later'}, subs, 5, vi.fn(), () => true).disabled).toBe(true)
  })
})
