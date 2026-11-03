import {expect,it} from 'vitest'
import {countUnread} from './counts'
import type {Entry} from './reader'
it('counts unread per feed and independent shelves without counting Done or seen',()=>{
  const entries=[['unseen','none',true,1],['unseen','later',true,2],['unseen','done',false,1],['seen','none',false,1]].map(([seen,shelf,kept,subscription_id])=>({subscription_id,deleted:false,state:{seen:{value:seen},shelf:{value:shelf},kept:{value:kept}}})) as Entry[]
  expect(countUnread(entries)).toEqual({byFeed:{'1':1,'2':1},later:1,kept:2})
})
