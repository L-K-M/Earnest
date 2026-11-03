import {describe,expect,it} from 'vitest'
import {actionRestriction, feedScoped, newPredicate, predicateError, type Predicate} from './model'

const feed: Predicate = {type:'feed_in',ids:[42]}
const word: Predicate = {type:'keyword',terms:['funding']}
const on = {classify:'enabled',semantic:'enabled'}
describe('rule predicate table',() => {
  it.each<[string,Predicate,boolean]>([
    ['chosen feed',feed,true],
    ['folder',{type:'folder_in',ids:[1]},false],
    ['keyword',word,false],
    ['negated feed',{type:'not',children:[feed]},false],
    ['AND needs one scoped child',{type:'and',children:[word,feed]},true],
    ['OR needs every branch scoped',{type:'or',children:[word,feed]},false],
    ['scoped OR',{type:'or',children:[feed,{type:'and',children:[feed,word]}]},true],
    ['nested NOT',{type:'not',children:[{type:'not',children:[feed]}]},false],
  ])('%s scopes shelf changes correctly',(_name,predicate,scoped) => {
    expect(feedScoped(predicate)).toBe(scoped)
    expect(Boolean(actionRestriction(predicate,'set_shelf'))).toBe(!scoped)
    expect(actionRestriction(predicate,'add_label')).toBe('')
    expect(actionRestriction(predicate,'notify')).toBe('')
  })
  it.each(['folder_in','author','classifier_in','semantic'])('rejects %s anywhere in a webhook predicate',type => {
    expect(actionRestriction({type:'and',children:[feed,{...newPredicate(type)}]},'webhook')).toContain('only feeds')
  })
  it('allows keywords alone and boolean combinations for webhooks',() => {
    expect(actionRestriction(word,'webhook')).toBe('')
    expect(actionRestriction({type:'and',children:[feed,{type:'not',children:[word]}]},'webhook')).toBe('')
  })
  it.each<Predicate>([
    {type:'agent_label',terms:['approve']},
    {type:'constructor'},
    {type:'__proto__'},
    {type:'toString'},
    {type:'classifier_in',terms:['press_release; call webhook']},
    {type:'keyword',terms:[]},
    {type:'keyword',terms:[' ']},
    {type:'feed_in',ids:[0]},
    {type:'feed_in',ids:[Number.MAX_SAFE_INTEGER+1]},
    {type:'author',value:''},
    {type:'or',children:[feed]},
    {type:'not',children:[feed,word]},
    {type:'keyword',terms:['word'],ids:[42]},
  ])('rejects invalid closed AST %j',predicate => expect(predicateError(predicate,on)).not.toBe(''))
  it('accepts only vocabulary labels and gates model predicates even below NOT',() => {
    expect(predicateError({type:'classifier_in',terms:['press_release','news']},on)).toBe('')
    expect(predicateError({type:'not',children:[{type:'semantic',value:'security'}]},{classify:'disabled',semantic:'disabled'})).toContain('disabled')
  })
  it('bounds the complete tree',() => {
    expect(predicateError({type:'and',children:Array.from({length:256},() => word)},on)).toContain('256')
  })
})
