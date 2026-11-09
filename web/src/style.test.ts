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
  // The plain values live in the same selector's base rule, just before the
  // guard; other theme blocks declaring a token do not count.
  const selector = ':root, [data-theme=light], [data-theme=dark] {'
  const baseStart = css.lastIndexOf(selector, guard)
  expect(baseStart).toBeGreaterThanOrEqual(0)
  const base = css.slice(baseStart, css.indexOf('}', baseStart))
  // Every token the guard themes, including composite ones such as --shadow.
  const names = [...guarded.matchAll(/--([\w-]+):/g)].map(match => match[1])
  expect(names.length).toBeGreaterThan(0)
  for (const name of names) expect(base, `plain value for --${name}`).toMatch(new RegExp(`--${name}:\\s*[^;]*[^;\\s]`))
  // Where the whole value is light-dark(), the plain value is its light half.
  for (const [, name, light] of guarded.matchAll(/--([\w-]+):\s*light-dark\(([^,]+(?:\([^)]*\))?[^,]*),/g)) {
    expect(base, `plain value for --${name}`).toContain(`--${name}: ${light.trim()};`)
  }
})

it('lets the sync indicator shrink so its ellipsis engages', () => {
  expect(css).toMatch(/\.sync-indicator span\s*\{[^}]*min-width:\s*0/)
})
