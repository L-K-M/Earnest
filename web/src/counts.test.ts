import {expect,it,vi} from 'vitest'
import {readUnreadCounts} from './counts'
import {request} from './request'
vi.mock('./request',()=>({request:vi.fn()}))

it('maps the server unread counts to per-feed counts and shelf totals',async()=>{
  vi.mocked(request).mockResolvedValue({by_feed:{'1':2,'2':1},later:1,kept:3,total:6})
  const controller=new AbortController()
  await expect(readUnreadCounts(controller.signal)).resolves.toEqual({byFeed:{'1':2,'2':1},later:1,kept:3})
  expect(request).toHaveBeenCalledWith('/v1/entries:counts',{signal:controller.signal})
})

it('tolerates a counts response with no rows',async()=>{
  vi.mocked(request).mockResolvedValue({by_feed:{},later:0,kept:0,total:0})
  await expect(readUnreadCounts(new AbortController().signal)).resolves.toEqual({byFeed:{},later:0,kept:0})
})
