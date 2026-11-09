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

const toneNames = ['orange', 'amber', 'plum', 'purple', 'blue', 'green']

function ruleBody(selector: string, from = 0): string {
  const start = css.indexOf(selector, from)
  expect(start, `missing rule ${selector}`).toBeGreaterThanOrEqual(0)
  const open = css.indexOf('{', start)
  return css.slice(open + 1, css.indexOf('}', open))
}

function tokens(body: string): Map<string, string> {
  return new Map([...body.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)].map(match => [match[1], match[2].trim()]))
}

it('defines the icon tones and the sign-in glow in every theme block', () => {
  const guard = css.indexOf('@supports (color: light-dark(#000, #fff))')
  const base = tokens(ruleBody(':root, [data-theme=light], [data-theme=dark] {'))
  const guarded = tokens(ruleBody(':root, [data-theme=light], [data-theme=dark] {', guard))
  const black = tokens(ruleBody('[data-theme=black]'))
  const eink = tokens(ruleBody('[data-theme=eink]'))
  for (const name of [...toneNames, 'glow']) {
    expect(base.has(name), `base rule has --${name}`).toBe(true)
    expect(guarded.has(name), `@supports rule has --${name}`).toBe(true)
    expect(black.has(name), `black theme has --${name}`).toBe(true)
    expect(eink.has(name), `e-ink theme has --${name}`).toBe(true)
  }
  for (const name of toneNames) expect(eink.get(name)).toBe('#000')
  expect(black.get('glow')).toBe('transparent')
  expect(eink.get('glow')).toBe('transparent')
})

// WCAG 2 relative luminance, then the contrast ratio between two hex colors.
function luminance(hex: string): number {
  const value = hex.replace('#', '')
  const [r, g, b] = [0, 2, 4].map(offset => {
    const srgb = parseInt(value.slice(offset, offset + 2), 16) / 255
    return srgb <= 0.04045 ? srgb / 12.92 : ((srgb + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

function lightDark(value: string): [string, string] {
  const inner = value.replace(/^light-dark\(/, '').replace(/\)$/, '')
  const comma = inner.indexOf(',')
  return [inner.slice(0, comma).trim(), inner.slice(comma + 1).trim()]
}

it('keeps every icon tone legible on every surface', () => {
  const guard = css.indexOf('@supports (color: light-dark(#000, #fff))')
  const base = tokens(ruleBody(':root, [data-theme=light], [data-theme=dark] {'))
  const guarded = tokens(ruleBody(':root, [data-theme=light], [data-theme=dark] {', guard))
  const lightSurfaces = ['#f8f6ef', '#ffffff', '#f0ede3', '#f9e7d4', '#f4ede2']
  const darkSurfaces = ['#1c1c1a', '#242424', '#2c2c2a', '#000000', '#3a302a', '#2c2825']
  for (const name of toneNames) {
    const light = base.get(name)
    const dark = guarded.get(name)
    expect(light, `--${name} light half`).toBeTruthy()
    expect(dark, `--${name} dark half`).toBeTruthy()
    const [, darkValue] = lightDark(dark!)
    for (const surface of lightSurfaces) expect(contrast(light!, surface), `${name} on ${surface}`).toBeGreaterThanOrEqual(3)
    for (const surface of darkSurfaces) expect(contrast(darkValue, surface), `${name} on ${surface}`).toBeGreaterThanOrEqual(4.5)
  }
})
