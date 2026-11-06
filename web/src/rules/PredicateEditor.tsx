import {useEffect, useRef} from 'react'
import type {components} from '../api.generated'
import type {Subscription} from '../reader'
import {classifierVocabulary, newPredicate, normalizeTerms, type Predicate} from './model'

type Props = {value: Predicate; onChange: (value: Predicate) => void; feeds: Subscription[]; folders: components['schemas']['Folder'][]; pipeline: components['schemas']['PipelineStatus']; path?: string; depth?: number}

// KeywordTerms keeps the textarea uncontrolled so a trailing newline survives
// typing, while the predicate only ever sees normalized terms. External
// predicate changes (for example Compile) are written straight into the DOM.
function KeywordTerms({ariaLabel, terms, onChange}: {ariaLabel: string; terms: string[]; onChange: (terms: string[]) => void}) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const emitted = useRef(terms.join('\n'))
  useEffect(() => {
    const joined = terms.join('\n')
    if (joined !== emitted.current) {
      emitted.current = joined
      if (ref.current) ref.current.value = joined
    }
  }, [terms])
  return <label>Keywords (one per line)<textarea ref={ref} aria-label={ariaLabel} defaultValue={terms.join('\n')} onChange={event => {
    const next = normalizeTerms(event.target.value.split('\n'))
    emitted.current = next.join('\n')
    onChange(next)
  }} /></label>
}
const types = [['feed_in','Feeds you choose'], ['folder_in','Folders'], ['keyword','Keywords you write'], ['author','Author'], ['classifier_in','Classifier label'], ['semantic','Semantic match'], ['and','All conditions'], ['or','Any condition'], ['not','Not']] as const

export function PredicateEditor({value, onChange, feeds, folders, pipeline, path='Condition', depth=0}: Props) {
  const children = value.children ?? []
  const choices = value.type === 'feed_in' ? [...new Map(feeds.filter(f => !f.deleted).map(f => [f.feed_id, {id:f.feed_id, name:f.title || f.url}])).values()] : folders.filter(f => !f.deleted)
  function changeChild(index: number, child: Predicate) { onChange({...value, children:children.map((old, i) => i === index ? child : old)}) }
  return <fieldset><legend>{path}</legend>
    <label>Match<select aria-label={`${path} type`} value={value.type} onChange={event => onChange(newPredicate(event.target.value))}>{types.filter(([type]) => (type !== 'semantic' || pipeline.semantic === 'enabled' || value.type === type) && (type !== 'classifier_in' || pipeline.classify === 'enabled' || value.type === type)).map(([type, title]) => <option key={type} value={type}>{title}</option>)}</select></label>
    {['feed_in','folder_in'].includes(value.type) && <label>Choose {value.type === 'feed_in' ? 'feeds' : 'folders'}<select multiple aria-label={`${path} selection`} value={(value.ids ?? []).map(String)} onChange={event => onChange({...value, ids:Array.from(event.target.selectedOptions, option => Number(option.value))})}>{choices.map(choice => <option key={choice.id} value={choice.id}>{choice.name}</option>)}</select></label>}
    {value.type === 'keyword' && <KeywordTerms ariaLabel={`${path} keywords`} terms={value.terms ?? []} onChange={terms => onChange({...value, terms})} />}
    {value.type === 'classifier_in' && <label>Classifier labels<select multiple aria-label={`${path} classifier labels`} value={value.terms ?? []} onChange={event => onChange({...value, terms:Array.from(event.target.selectedOptions, option => option.value)})}>{classifierVocabulary.map(label => <option key={label}>{label}</option>)}</select></label>}
    {(value.type === 'author' || value.type === 'semantic') && <label>{value.type === 'author' ? 'Author name' : 'Semantic description'}<input aria-label={`${path} value`} value={value.value ?? ''} onChange={event => onChange({...value, value:event.target.value})}/></label>}
    {children.map((child, i) => <div key={i}><PredicateEditor value={child} onChange={next => changeChild(i,next)} feeds={feeds} folders={folders} pipeline={pipeline} path={`${path}.${i+1}`} depth={depth+1}/>{value.type !== 'not' && <button type="button" disabled={children.length <= 2} onClick={() => onChange({...value, children:children.filter((_,index) => index !== i)})}>Remove {path}.{i+1}</button>}</div>)}
    {['and','or'].includes(value.type) && <button type="button" disabled={depth >= 20} onClick={() => onChange({...value, children:[...children,newPredicate('keyword')]})}>Add condition to {path}</button>}
  </fieldset>
}
