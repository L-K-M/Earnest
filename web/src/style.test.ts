import {readFileSync} from 'node:fs'
import {expect, it} from 'vitest'

const css = readFileSync(new URL('./style.css', import.meta.url), 'utf8')

// Browsers without light-dark() invalidate the whole custom property, so each
// token needs a plain fallback declaration before the light-dark() one.
it('gives every light-dark token a plain fallback', () => {
  const lines = css.split('\n')
  const themed = lines.filter(line => line.includes('light-dark('))
  expect(themed.length).toBeGreaterThan(0)
  for (const line of themed) {
    const name = line.match(/--([\w-]+)\s*:/)?.[1]
    expect(name, line).toBeTruthy()
    const previous = [...lines.slice(0, lines.indexOf(line))].reverse().find(value => value.trim().length > 0)
    expect(previous, `fallback for ${name}`).toContain(`--${name}:`)
  }
})

it('lets the sync indicator shrink so its ellipsis engages', () => {
  expect(css).toMatch(/\.sync-indicator span\s*\{[^}]*min-width:\s*0/)
})
