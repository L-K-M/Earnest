import {describe,expect,it} from 'vitest'
import {cacheKind,evictionKeys,warmContentPaths,warmLimit} from './policy'

const origin = 'https://reader.test'
describe('offline cache allowlist',() => {
  it.each([
    ['/','shell'],['/assets/app-123.js','shell'],['/assets/app-123.css','shell'],
    ['/icon-192.png','shell'],['/wordmark.png','shell'],['/icon-512.png',null],
    ['/v1/me','resource'],['/v1/settings','resource'],['/v1/subscriptions','resource'],
    ['/v1/positions/feed%3A7','reading'],['/v1/entries?view=river&after_seq=7','reading'],
    ['/v1/entries/42/content','content'],
    ['/v1/state:batch',null],['/v1/events',null],['/v1/bootstrap',null],['/v1/changes?cursor=1',null],
    ['/web/content-url/42',null],['/v1/tokens',null],['/v1/session/login',null],
    ['/v1/entries/42/annotations',null],['/v1/entries/0/content',null],
    ['https://publisher.test/article',null],['https://content.test/content/42?sig=secret',null],
  ])('classifies %s as %s',(path,kind) => expect(cacheKind(path,origin)).toBe(kind))
  it.each(['POST','PUT','PATCH','DELETE','HEAD'])('never caches a %s as a successful read',method => {
    expect(cacheKind('/v1/entries?ids=1',origin,method)).toBeNull()
  })
})
describe('bounded oldest-first eviction',() => {
  it('preserves existing items below the bound',() => expect(evictionKeys(['a','b'],'c',3)).toEqual([]))
  it('evicts the oldest on overflow',() => expect(evictionKeys(['a','b'],'c',2)).toEqual(['a']))
  it('moves refreshed copies to the end',() => expect(evictionKeys(['a','b','c'],'a',2)).toEqual(['b']))
  it('handles zero capacity and overfull old caches',() => {
    expect(evictionKeys(['a'],'b',0)).toEqual(['a','b'])
    expect(evictionKeys(['a','b','c'],'d',1)).toEqual(['a','b','c'])
  })
  it('selects safe entry IDs only, deduplicates and caps each warmup',() => {
    expect(warmContentPaths({entries:[null,{}, {id:1}, {id:1}, {id:'2'}, {id:3,deleted:true}, {id:Number.MAX_SAFE_INTEGER+1}, {id:-1}]})).toEqual(['/v1/entries/1/content'])
    const paths = warmContentPaths({entries:Array.from({length:300},(_,i) => ({id:i+1}))})
    expect(paths).toHaveLength(warmLimit)
    expect(paths[0]).toBe('/v1/entries/1/content')
    expect(warmContentPaths(null)).toEqual([])
  })
})
