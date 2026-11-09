import {readFileSync} from 'node:fs'
import {expect, it} from 'vitest'

// Comments may name light-dark(); only declarations matter here.
const css = readFileSync(new URL('./style.css', import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')

// Custom properties accept any value, so a light-dark() declaration in the same
// rule as a plain one would win and then fail at every var() in browsers
// without light-dark(). Plain values stay outside, light-dark() only behind
// @supports, and every themed token has a plain light value.
it('keeps light-dark tokens behind @supports with plain light values outside', () => {
  const guard = css.indexOf('@supports (color: light-dark(#000, #fff))')
  expect(guard).toBeGreaterThan(0)
  const guarded = css.slice(guard, css.indexOf('\n}\n', guard))
  const outside = css.slice(0, guard) + css.slice(guard + guarded.length)
  expect(outside).not.toContain('light-dark(')
  const themed = [...guarded.matchAll(/--([\w-]+):\s*light-dark\(([^,]+(?:\([^)]*\))?[^,]*),/g)]
  expect(themed.length).toBeGreaterThan(0)
  for (const [, name, light] of themed) {
    expect(outside, `plain value for --${name}`).toContain(`--${name}: ${light.trim()};`)
  }
})

it('lets the sync indicator shrink so its ellipsis engages', () => {
  expect(css).toMatch(/\.sync-indicator span\s*\{[^}]*min-width:\s*0/)
})
