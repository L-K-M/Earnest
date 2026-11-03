// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {Push, Tokens, Webhooks} from './Account'
import {request} from '../request'
vi.mock('../request', () => ({request:vi.fn()}))
function mount(node:React.ReactNode) {return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{node}</QueryClientProvider>)}
afterEach(() => {cleanup();vi.resetAllMocks();history.replaceState(null, '', '/')})
it('offers only agent scopes, creates a token and shows its secret once', async () => {
  vi.mocked(request).mockImplementation(async (_path, options) => options?.method === 'POST' ? {secret:'earnest_agent_secret'} : [])
  mount(<Tokens />)
  const form = screen.getByRole('group', {name:'Agent scopes'})
  for (const forbidden of ['feeds:write','webhooks:write','tokens:write','devices:write','admin','state:write']) expect(within(form).queryByText(new RegExp(`^${forbidden}:`))).toBeNull()
  expect((screen.getByLabelText(/notify:urgent/) as HTMLInputElement).checked).toBe(false)
  fireEvent.change(screen.getByLabelText('Name'), {target:{value:'Scout'}})
  fireEvent.click(screen.getByLabelText(/entries:read/))
  fireEvent.click(screen.getByRole('button',{name:'Create agent token'}))
  await screen.findByText('earnest_agent_secret')
  const call = vi.mocked(request).mock.calls.find(([, options]) => options?.method === 'POST')!
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({kind:'agent',name:'Scout',audience:'mcp',scopes:['entries:read'],daily_budget:3})
  fireEvent.click(screen.getByText('Done'));expect(screen.queryByText('earnest_agent_secret')).toBeNull()
})
it('approves a code supplied in the URL and confirms revocation', async () => {
  history.replaceState(null, '', '/device?user_code=ABCD-1234')
  vi.mocked(request).mockImplementation(async (_path, options) => options?.method ? {} : [{id:1,name:'Phone',kind:'app',audience:'api',scopes:[],daily_budget:0}])
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  mount(<Tokens />)
  expect((screen.getByLabelText('Device code') as HTMLInputElement).value).toBe('ABCD-1234')
  fireEvent.click(screen.getByText('Approve device'));await screen.findByText('Device approved.')
  expect(vi.mocked(request)).toHaveBeenCalledWith('/v1/devices/approve', expect.objectContaining({body:JSON.stringify({user_code:'ABCD-1234'})}))
  fireEvent.click(await screen.findByText('Revoke'));await waitFor(() => expect(vi.mocked(request)).toHaveBeenCalledWith('/v1/tokens/1', {method:'DELETE'}))
})
it('saves push settings without replacing an existing token and removes a device', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => options?.method ? {} : path === '/v1/push-settings' ? {timezone:'UTC',show_text:false,token_set:true,rev:1} : [{id:2,kind:'webpush',endpoint:'https://push.example/a',show_text:false}])
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  mount(<Push />);await screen.findByLabelText('Timezone')
  fireEvent.change(screen.getByLabelText('ntfy topic'), {target:{value:'news'}})
  fireEvent.click(screen.getByText('Save push settings'));await screen.findByText('Push settings saved.')
  const call = vi.mocked(request).mock.calls.find(([path, options]) => path === '/v1/push-settings' && options?.method === 'PUT')!
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({ntfy_topic:'news',timezone:'UTC',show_text:false})
  expect(JSON.parse(String(call[1]?.body))).not.toHaveProperty('ntfy_token')
  fireEvent.click(await screen.findByText('Remove'));await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path, options]) => path.startsWith('/v1/devices/2?op_id=') && options?.method === 'DELETE')).toBe(true))
})
it('subscribes the browser with VAPID and registers its keys', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => options?.method ? {} : path === '/v1/push-settings' ? {timezone:'UTC',show_text:false,token_set:false,rev:1} : path === '/v1/push/vapid-public-key' ? {public_key:'AQID'} : [])
  const subscribe = vi.fn().mockResolvedValue({toJSON:()=>({endpoint:'https://push.example/a',keys:{p256dh:'key',auth:'auth'}})})
  Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{register:vi.fn().mockResolvedValue({pushManager:{subscribe}})}})
  Object.defineProperty(window,'PushManager',{configurable:true,value:function(){}})
  mount(<Push />);fireEvent.click(screen.getByText('Subscribe this browser'));await screen.findByText('This browser is registered.')
  expect(subscribe).toHaveBeenCalledWith(expect.objectContaining({userVisibleOnly:true,applicationServerKey:new Uint8Array([1,2,3])}))
  const call=vi.mocked(request).mock.calls.find(([path,options])=>path==='/v1/devices' && options?.method==='POST')!
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({kind:'webpush',endpoint:'https://push.example/a',p256dh:'key',auth_secret:'auth',show_text:false})
})
it('creates, disables and deletes webhooks', async () => {
  vi.mocked(request).mockImplementation(async (_path, options) => options?.method === 'POST' ? {secret:'whsec_secret'} : options?.method ? {} : [{id:4,url:'https://example.test/hook',events:['feed.failing'],active:true,consecutive_failures:0}])
  vi.spyOn(window,'confirm').mockReturnValue(true)
  mount(<Webhooks />);fireEvent.change(screen.getByLabelText('URL'),{target:{value:'https://example.test/new'}});fireEvent.click(screen.getByLabelText('entry.matched'));fireEvent.click(screen.getByRole('button',{name:'Create webhook'}));await screen.findByText('whsec_secret')
  const call=vi.mocked(request).mock.calls.find(([, options])=>options?.method==='POST')!
  expect(JSON.parse(String(call[1]?.body))).toMatchObject({url:'https://example.test/new',events:['entry.matched']})
  fireEvent.click(await screen.findByText('Disable'));await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path,options])=>path==='/v1/webhooks/4' && options?.method==='PATCH')).toBe(true))
  fireEvent.click(screen.getByText('Delete'));await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path,options])=>path.startsWith('/v1/webhooks/4?op_id=') && options?.method==='DELETE')).toBe(true))
})
