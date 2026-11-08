import {describe, expect, it} from 'vitest'
import {activityHref, parseRoute, routeHref, routeSection, searchHref, viewHref, type Route} from './routes'

describe('route contract', () => {
  it.each<[string, string, Route]>([
    ['/', '', {name: 'river', view: {kind: 'today'}}],
    ['/today', '', {name: 'river', view: {kind: 'today'}}],
    ['/later/', '', {name: 'river', view: {kind: 'later'}}],
    ['/edition/articles/42', '', {name: 'river', view: {kind: 'edition'}, entry: 42}],
    ['/feeds/7', '', {name: 'river', view: {kind: 'feed', id: 7}}],
    ['/folders/3/articles/9', '', {name: 'river', view: {kind: 'folder', id: 3}, entry: 9}],
    ['/labels/5', '', {name: 'river', view: {kind: 'label', id: 5}}],
    ['/feeds', '', {name: 'feeds'}],
    ['/search', '?q=%20rust%20&mode=hybrid&feed=2&folder=4&limit=50', {name: 'search', search: {q: 'rust', mode: 'hybrid', feed: 2, folder: 4, limit: 50}}],
    ['/search/saved/12', '', {name: 'search', search: {q: ''}, saved: 12}],
    ['/ask', '', {name: 'ask'}],
    ['/rules', '', {name: 'rules'}],
    ['/rules/new', '', {name: 'rules', rule: 'new'}],
    ['/rules/8', '', {name: 'rules', rule: 8}],
    ['/activity', '?agent=3&action=state.updated&since=2026-11-01', {name: 'activity', filters: {agent: '3', action: 'state.updated', since: '2026-11-01'}}],
    ['/notifications/4', '', {name: 'notifications', id: 4}],
    ['/settings', '', {name: 'settings', device: false}],
    ['/device', '?user_code=ABCD-EFGH', {name: 'settings', device: true}],
  ])('parses %s%s', (path, search, route) => {
    expect(parseRoute(path, search)).toEqual(route)
  })

  it.each(['/feeds/0', '/feeds/abc', '/feeds/7/extra', '/today/articles/x', '/today/articles/1/2', '/rules/new/1', '/nowhere', '/search/saved/', '/feeds/99999999999999999', '/%E0%A4%A'])('reports %s as not found', path => {
    expect(parseRoute(path).name).toBe('not-found')
  })

  it('drops filters it cannot use instead of passing them on', () => {
    expect(parseRoute('/search', '?q=x&mode=semantic&feed=-1&limit=lots')).toEqual({name: 'search', search: {q: 'x'}})
    expect(parseRoute('/activity', '?agent=me&since=yesterday')).toEqual({name: 'activity', filters: {}})
  })

  it('formats every route back to a path that parses to the same route', () => {
    const routes: Route[] = [
      {name: 'river', view: {kind: 'today'}},
      {name: 'river', view: {kind: 'kept'}, entry: 3},
      {name: 'river', view: {kind: 'feed', id: 7}, entry: 11},
      {name: 'search', search: {q: 'a b&c', mode: 'hybrid', feed: 2}},
      {name: 'search', search: {q: 'x'}, saved: 4},
      {name: 'rules', rule: 'new'},
      {name: 'activity', filters: {action: 'notification.created'}},
      {name: 'notifications', id: 9},
      {name: 'settings', device: true},
    ]
    for (const route of routes) {
      const href = new URL(routeHref(route), 'https://reader.test')
      expect(parseRoute(href.pathname, href.search)).toEqual(route)
    }
  })

  it('builds the links the sidebar and lists use', () => {
    expect(viewHref({kind: 'folder', id: 2})).toBe('/folders/2')
    expect(viewHref({kind: 'done'}, 5)).toBe('/done/articles/5')
    expect(searchHref({q: ''})).toBe('/search')
    expect(activityHref({})).toBe('/activity')
  })

  it('keeps a list section current while one of its articles is open', () => {
    expect(routeSection(parseRoute('/feeds/7/articles/2'))).toBe(routeSection(parseRoute('/feeds/7')))
    expect(routeSection(parseRoute('/search/saved/3'))).not.toBe(routeSection(parseRoute('/search')))
  })
})
