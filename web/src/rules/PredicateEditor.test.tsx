// @vitest-environment jsdom
import {afterEach, describe, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {PredicateEditor} from './PredicateEditor'
import type {components} from '../api.generated'

afterEach(() => cleanup())

const pipeline = {ask:'enabled', blurb:'enabled', classify:'enabled', compile_rule:'enabled', edition:'enabled', embed:'enabled', semantic:'enabled', summary:'enabled', watch:'enabled'} as components['schemas']['PipelineStatus']

describe('PredicateEditor keyword terms', () => {
  it('keeps a trailing newline in the textarea while normalizing terms', () => {
    const onChange = vi.fn()
    render(<PredicateEditor value={{type:'keyword',terms:[]}} onChange={onChange} feeds={[]} folders={[]} pipeline={pipeline} />)
    const textarea = screen.getByLabelText('Condition keywords') as HTMLTextAreaElement
    fireEvent.change(textarea, {target: {value: 'funding\n'}})
    // The raw text keeps its newline so the next line can be typed.
    expect(textarea.value).toBe('funding\n')
    expect(onChange).toHaveBeenLastCalledWith({type:'keyword',terms:['funding']})
  })

  it('drops blank lines and trims whitespace', () => {
    const onChange = vi.fn()
    render(<PredicateEditor value={{type:'keyword',terms:[]}} onChange={onChange} feeds={[]} folders={[]} pipeline={pipeline} />)
    const textarea = screen.getByLabelText('Condition keywords') as HTMLTextAreaElement
    fireEvent.change(textarea, {target: {value: ' funding \n\nai\n'}})
    expect(onChange).toHaveBeenLastCalledWith({type:'keyword',terms:['funding','ai']})
  })

  it('reflects an externally replaced predicate', () => {
    const first = vi.fn()
    const {rerender} = render(<PredicateEditor value={{type:'keyword',terms:['old']}} onChange={first} feeds={[]} folders={[]} pipeline={pipeline} />)
    const textarea = screen.getByLabelText('Condition keywords') as HTMLTextAreaElement
    expect(textarea.value).toBe('old')
    rerender(<PredicateEditor value={{type:'keyword',terms:['new','terms']}} onChange={first} feeds={[]} folders={[]} pipeline={pipeline} />)
    expect(textarea.value).toBe('new\nterms')
  })
})
