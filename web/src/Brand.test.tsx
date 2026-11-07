// @vitest-environment jsdom
import {render,screen} from '@testing-library/react'
import {expect,it} from 'vitest'
import {Brand} from './Brand'

it('names the app in a level-one heading and keeps the icon decorative',() => {
  const {container} = render(<Brand />)
  expect(screen.getByRole('heading',{level:1,name:'Earnest'})).toBeTruthy()
  expect(container.querySelector('img')?.getAttribute('alt')).toBe('')
})
