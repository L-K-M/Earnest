// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, render, screen} from '@testing-library/react'
import {defaultSettings} from './Settings'
import {SettingsPage} from './SettingsPage'

vi.mock('./Settings', async importOriginal => ({
  ...await importOriginal<typeof import('./Settings')>(),
  Settings: () => null,
}))
vi.mock('./labels/LabelSettings', () => ({LabelSettings: () => null}))
vi.mock('./account/Account', () => ({Push: () => null, Tokens: () => null, Webhooks: () => null}))
vi.mock('./integrations/Integrations', () => ({Integrations: () => <h2>Article integration controls</h2>}))
vi.mock('./llm/LLMSettings', () => ({LLMSettings: () => <h2>LLM service controls</h2>}))

afterEach(cleanup)

it('keeps article integrations and LLM services reachable on the same Settings page', () => {
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)

  expect(screen.getByRole('link', {name: 'Article integrations'}).getAttribute('href')).toBe('/settings#integrations')
  expect(screen.getByRole('link', {name: 'LLM services'}).getAttribute('href')).toBe('/settings#llm')
  expect(screen.getByRole('heading', {name: 'Article integration controls'}).closest('.card')?.id).toBe('integrations')
  expect(screen.getByRole('heading', {name: 'LLM service controls'}).closest('.card')?.id).toBe('llm')
})
