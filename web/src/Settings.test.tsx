// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {Settings,defaultSettings,settingsInput} from './Settings'
import {request} from './request'
vi.mock('./request',()=>({request:vi.fn()}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
it('saves settings without sending the read-only revision back to strict API validation',async()=>{
  const onSaved=vi.fn()
  vi.mocked(request).mockResolvedValue({...defaultSettings,rev:12,single_key_shortcuts:false})
  render(<Settings settings={{...defaultSettings,rev:11}} onSaved={onSaved}/> )
  fireEvent.click(screen.getByLabelText('Single-key shortcuts'))
  fireEvent.click(screen.getByRole('button',{name:'Save settings'}))
  await waitFor(()=>expect(onSaved).toHaveBeenCalled())
  const call=vi.mocked(request).mock.calls[0]
  const body=JSON.parse(String(call[1]?.body)) as Record<string,unknown>
  expect(body.single_key_shortcuts).toBe(false)
  expect(body).not.toHaveProperty('rev')
  expect(body.op_id).toEqual(expect.any(String))
})
it('preserves feed overrides while replacing account preferences',()=>{
  expect(settingsInput({...defaultSettings,rev:12,feed_overrides:{'9':{measure:72}}},'save')).toEqual({...defaultSettings,feed_overrides:{'9':{measure:72}},op_id:'save'})
})
