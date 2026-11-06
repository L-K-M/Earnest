// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {ActivityScreen, Notifications, SessionChanges, TodayAgents} from './Surfaces'
import {request, RequestError} from '../request'
vi.mock('../request', async importOriginal => ({...await importOriginal<typeof import('../request')>(), request:vi.fn()}))
const hostile = '<img src=x onerror=alert(1)> <script>alert(1)</script> javascript:alert(1) [click](https://evil.test) https://other.test'
const row = {id:5,principal_id:2,principal_name:'Scout',action:'state.updated',target:{id:9},before:{entry_id:9,seen:{value:'unseen'}},after:{entry_id:9,seen:{value:'seen'}},generation:'g',created_at:'2026-01-01T00:00:00Z',next_action:'revert'}
const brief = {id:3,principal_id:2,kind:'brief',author_name:'Scout',created_at:'2026-01-01T00:00:00Z',entry_ids:[],value:hostile,rev:1,untrusted:true,deleted:false}
const notification = {id:8,principal_id:2,note:hostile,entry_ids:[],category:'alert',status:'held',muted:false,budget:{used_today:1,limit_today:3},why:{sender:'Scout',budget:{used_today:1,limit_today:3},matching_rule_id:4},generation:'g',created_at:'2026-01-01T00:00:00Z',replay:false}
function mount(node:React.ReactNode) {return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{node}</QueryClientProvider>)}
afterEach(()=>{cleanup();vi.resetAllMocks();localStorage.clear()})
it('renders hostile brief text inert and remembers collapse',async()=>{
 vi.mocked(request).mockImplementation(async path=>path.startsWith('/v1/briefs')?[brief]:path.startsWith('/v1/activity?')?{activities:[]} : [])
 const view=mount(<TodayAgents since="2026-01-01T00:00:00Z" onOpen={vi.fn()}/>); await screen.findByText(hostile)
 expect(view.container.querySelectorAll('a,img,script')).toHaveLength(0)
 fireEvent.click(screen.getByText(`Scout · ${new Date(brief.created_at).toLocaleString()}`)); await waitFor(()=>expect(localStorage.getItem('earnest.brief.3')).toBe('closed'))
})
it('groups changes and explains a conflicting revert',async()=>{
 vi.mocked(request).mockImplementation(async (path,options)=>{if(options?.method==='POST') throw new RequestError(409,'conflict');return path.startsWith('/v1/briefs')?[]:path.startsWith('/v1/activity?')?{activities:[]}:[row]})
 mount(<TodayAgents since="2026-01-01T00:00:00Z" onOpen={vi.fn()}/>); fireEvent.click(await screen.findByText('While you were out'))
 expect(screen.getByText(/Article 9: seen: unseen to seen/)).toBeTruthy();fireEvent.click(screen.getByText('Revert'));await screen.findByRole('alert')
 expect((await screen.findByRole('alert')).textContent).toContain('You changed it since')
})
it('filters activity by agent name and shows an empty state',async()=>{
 vi.mocked(request).mockImplementation(async path=>path==='/v1/agents'?[{principal_id:2,name:'Scout',muted:false,used_today:0}]:path.startsWith('/v1/activity?')?{activities:[]}:[])
 mount(<ActivityScreen/>)
 const select=await screen.findByLabelText('Agent')
 await screen.findByText('Scout',{exact:true})
 expect(within(select).getByText('Scout')).toBeTruthy()
 fireEvent.change(select,{target:{value:'2'}})
 await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path])=>path.includes('principal_id=2'))).toBe(true))
 expect(await screen.findByText('No activity matches these filters.')).toBeTruthy()
})
it('shows an empty state when there is no activity',async()=>{
 vi.mocked(request).mockImplementation(async path=>path==='/v1/agents'?[]:path.startsWith('/v1/activity?')?{activities:[]}:[])
 mount(<ActivityScreen/>)
 expect(await screen.findByText('No activity yet.')).toBeTruthy()
})
it('shows filtered activity and session changes',async()=>{
 vi.mocked(request).mockImplementation(async path=>path==='/v1/agents'?[{principal_id:2,name:hostile,muted:true,used_today:3},{principal_id:3,name:'Other',muted:false,used_today:0}]:{activities:[row]})
 const view=mount(<ActivityScreen/>);expect(await screen.findByText('Scout')).toBeTruthy();expect(await screen.findByText(`${hostile} · Muted · 3 notifications today`)).toBeTruthy();expect(screen.getByText('Other · Unmuted · 0 notifications today')).toBeTruthy();expect(view.container.querySelectorAll('a,img,script')).toHaveLength(0);fireEvent.change(screen.getByLabelText('Action'),{target:{value:'state.updated'}})
 await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path])=>path.includes('action=state.updated'))).toBe(true));view.unmount()
 vi.mocked(request).mockResolvedValue([row]);mount(<SessionChanges since="2026-01-01T00:00:00Z"/>);expect(await screen.findByText('Agent changes this session:')).toBeTruthy()
})
it('shows notification provenance, inert note, mute controls and release',async()=>{
 vi.mocked(request).mockImplementation(async path=>path==='/v1/notifications/8'?notification:path.startsWith('/v1/notifications?')?[notification]:path==='/v1/bootstrap'?{generation:'g'}:{})
 const view=mount(<Notifications onOpen={vi.fn()} onOpenSettings={vi.fn()}/>);fireEvent.click(await screen.findByText(/Scout · alert/));await screen.findByText(hostile)
 expect(view.container.querySelectorAll('a,img,script')).toHaveLength(0);expect(screen.getByText(/Matching rule: 4/)).toBeTruthy();expect(screen.queryByText('Unmute Scout')).toBeNull()
 fireEvent.click(screen.getByText('Mute Scout'));await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path,opts])=>path==='/v1/agents/2/mute' && opts?.method==='PUT')).toBe(true))
 fireEvent.click(screen.getByText('Release current notifications'));await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path])=>path==='/v1/notifications:release')).toBe(true))
})
it('opens a cited entry and labels the brief from its own fields',async()=>{
 const cited={id:12,title:'Cited article'}
 vi.mocked(request).mockImplementation(async path=>path.startsWith('/v1/briefs')?[{...brief,entry_ids:[12]}]:path==='/v1/entries/12'?cited:[])
 const open=vi.fn();mount(<TodayAgents since="2026-01-01T00:00:00Z" onOpen={open}/>);fireEvent.click(await screen.findByText('Cited article'));expect(open).toHaveBeenCalledWith(cited);expect(screen.getByText(`Scout · ${new Date(brief.created_at).toLocaleString()}`)).toBeTruthy();expect(vi.mocked(request).mock.calls.some(([path])=>path.startsWith('/v1/activity?'))).toBe(false)
})
it('cancels a held notification from activity and can unmute',async()=>{
 const held={...row,action:'notification.created',target:{id:8},next_action:'cancel'}
 vi.mocked(request).mockImplementation(async (path,options)=>options?.method?{}:path.startsWith('/v1/activity?')?{activities:[held]}:path==='/v1/notifications/8'?{...notification,muted:true}:path.startsWith('/v1/notifications?')?[notification]:path==='/v1/agents'?[]:{})
 const view=mount(<ActivityScreen/>);fireEvent.click(await screen.findByText('Cancel'));await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path])=>path==='/v1/notifications/8:cancel')).toBe(true));view.unmount()
 mount(<Notifications onOpen={vi.fn()} onOpenSettings={vi.fn()}/>);fireEvent.click(await screen.findByText(/Scout · alert/));fireEvent.click(await screen.findByText('Unmute Scout'));expect(screen.queryByText('Mute Scout')).toBeNull();await waitFor(()=>expect(vi.mocked(request).mock.calls.some(([path,opts])=>path.startsWith('/v1/agents/2/mute?op_id=') && opts?.method==='DELETE')).toBe(true))
})
it('explains a failed notification and links to push settings',async()=>{
 const failed={...notification,status:'failed'}
 const openSettings=vi.fn()
 vi.mocked(request).mockImplementation(async path=>path==='/v1/notifications/8'?failed:path.startsWith('/v1/notifications?')?[failed]:path==='/v1/bootstrap'?{generation:'g'}:{})
 mount(<Notifications onOpen={vi.fn()} onOpenSettings={openSettings}/>)
 fireEvent.click(await screen.findByText(/Scout · alert/))
 await screen.findByText(/No push destination accepted/)
 fireEvent.click(screen.getByText('Check push settings'))
 expect(openSettings).toHaveBeenCalled()
})
