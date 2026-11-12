// @vitest-environment jsdom
import {cleanup,render,screen} from '@testing-library/react'
import {afterEach,expect,it} from 'vitest'
import {Brand} from './Brand'

afterEach(cleanup)

it('names the app in a level-one heading and keeps the icon decorative',() => {
  const {container} = render(<Brand />)
  expect(screen.getByRole('heading',{level:1,name:'Earnest'})).toBeTruthy()
  expect(container.querySelector('img')?.getAttribute('alt')).toBe('')
})

it('links the lockup home to the front page in the sidebar only',() => {
  const {container,rerender} = render(<Brand home />)
  const link = screen.getByRole('link',{name:'Earnest'})
  expect(link.getAttribute('href')).toBe('/front')
  expect(link.getAttribute('title')).toBe('Go to Front page')
  expect(screen.getByRole('heading',{level:1,name:'Earnest'}).contains(link)).toBe(true)
  rerender(<Brand />)
  expect(container.querySelector('a')).toBeNull()
})
