// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {Login} from './Login'

afterEach(cleanup)

function signIn(remember: boolean) {
  const onSignIn = vi.fn().mockResolvedValue(null)
  render(<Login onSignIn={onSignIn} />)
  fireEvent.change(screen.getByLabelText('Username'),{target:{value:'reader'}})
  fireEvent.change(screen.getByLabelText('Password'),{target:{value:'secret'}})
  const checkbox = screen.getByRole('checkbox',{name:'Remember me on this computer'})
  expect((checkbox as HTMLInputElement).checked).toBe(false)
  if (remember) fireEvent.click(checkbox)
  fireEvent.click(screen.getByRole('button',{name:'Sign in'}))
  return onSignIn
}

it('signs in for the standard session unless this computer is remembered',async () => {
  const onSignIn = signIn(false)
  await waitFor(() => expect(onSignIn).toHaveBeenCalledWith({username:'reader',password:'secret',remember:false}))
})

it('asks the server to remember this computer when the box is checked',async () => {
  const onSignIn = signIn(true)
  await waitFor(() => expect(onSignIn).toHaveBeenCalledWith({username:'reader',password:'secret',remember:true}))
})
