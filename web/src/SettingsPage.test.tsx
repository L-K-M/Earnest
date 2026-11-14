// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, render, screen, waitFor} from '@testing-library/react'
import {defaultSettings} from './Settings'
import {SettingsPage} from './SettingsPage'
import {resetLocation} from './router'

vi.mock('./Settings', async importOriginal => ({
  ...await importOriginal<typeof import('./Settings')>(),
  Settings: () => <h2>Reading preferences panel</h2>,
}))
vi.mock('./labels/LabelSettings', () => ({LabelSettings: () => <h2>Organization panel</h2>}))
vi.mock('./account/Account', () => ({Push: () => <h2>Notifications panel</h2>, Tokens: () => <><h2>Access panel</h2><input id="device-code" /></>, Webhooks: () => <h2>Webhooks panel</h2>}))
vi.mock('./integrations/Integrations', () => ({Integrations: () => <h2>Article integration controls</h2>}))
vi.mock('./llm/LLMSettings', () => ({LLMSettings: () => <h2>AI service controls</h2>}))

afterEach(cleanup)

const PANES = [
  ['Reading & appearance', 'reading', 'Reading preferences panel'],
  ['Organization', 'labels', 'Organization panel'],
  ['Account & devices', 'tokens', 'Access panel'],
  ['Notifications', 'push', 'Notifications panel'],
  ['Connections', 'webhooks', 'Webhooks panel'],
  ['AI', 'llm', 'AI service controls'],
] as const

it('opens only the selected settings pane and marks its deep link current', () => {
  resetLocation('/settings#llm')
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('heading', {name: 'AI service controls'})).toBeTruthy()
  expect(screen.queryByRole('heading', {name: 'Reading preferences panel'})).toBeNull()
  expect(screen.queryByRole('heading', {name: 'Article integration controls'})).toBeNull()
  expect(screen.getByRole('link', {name: 'AI'}).getAttribute('aria-current')).toBe('page')
  expect(screen.getByRole('link', {name: 'Reading & appearance'}).getAttribute('aria-current')).toBeNull()
  expect(document.getElementById('llm')).toBeTruthy()
})

it('retains the old integrations deep link as the Connections pane', () => {
  resetLocation('/settings#integrations')
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('link', {name: 'Connections'}).getAttribute('aria-current')).toBe('page')
  expect(screen.getByRole('heading', {name: 'Webhooks panel'})).toBeTruthy()
  expect(screen.getByRole('heading', {name: 'Article integration controls'})).toBeTruthy()
})

it('falls back to reading settings for an unknown section link', () => {
  resetLocation('/settings#unknown')
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('heading', {name: 'Reading preferences panel'})).toBeTruthy()
  expect(screen.getByRole('link', {name: 'Reading & appearance'}).getAttribute('aria-current')).toBe('page')
})

it.each(['constructor', 'toString', '__proto__'])('falls back for inherited property section %s', hash => {
  resetLocation(`/settings#${hash}`)
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('heading', {name: 'Reading preferences panel'})).toBeTruthy()
  expect(screen.getByRole('link', {name: 'Reading & appearance'}).getAttribute('aria-current')).toBe('page')
})

it('keeps every settings pane reachable through its existing hash deep link', () => {
  for (const [title, id, heading] of PANES) {
    resetLocation(`/settings#${id}`)
    render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
    expect(screen.getByRole('link', {name: title}).getAttribute('href')).toBe(`/settings#${id}`)
    expect(screen.getByRole('link', {name: title}).getAttribute('aria-current')).toBe('page')
    expect(screen.getByRole('heading', {name: heading}).closest('.card')?.id).toBe(id)
    cleanup()
  }
})

it('focuses the device code field when opened from the device link', async () => {
  resetLocation('/device')
  render(<SettingsPage device settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('heading', {name: 'Approve a device'})).toBeTruthy()
  const field = document.getElementById('device-code')
  expect(field).toBeTruthy()
  await waitFor(() => expect(document.activeElement).toBe(field))
})
