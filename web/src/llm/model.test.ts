import {expect, it} from 'vitest'
import {validateBaseURL, validateDimensions, validateMoney, validateTimeout} from './model'

it('accepts money up to the server maximum and rejects anything above it', () => {
  expect(validateMoney('0')).toBe('')
  expect(validateMoney('20')).toBe('')
  expect(validateMoney('20.000000')).toBe('')
  expect(validateMoney('99999999.999999')).toBe('')
  expect(validateMoney('100000000')).toMatch(/above the server maximum/)
  expect(validateMoney('99999999.9999999')).toMatch(/at most six decimals/)
  expect(validateMoney('-1')).toMatch(/non-negative/)
  expect(validateMoney('1.5.5')).toMatch(/at most six decimals/)
})

it('validates base URLs without credentials, query or fragment', () => {
  expect(validateBaseURL('https://api.openai.com/v1')).toBe('')
  expect(validateBaseURL('http://ollama:11434/v1')).toBe('')
  expect(validateBaseURL('https://user:pass@example.com/v1')).toMatch(/credentials/)
  expect(validateBaseURL('https://example.com/v1?x=1')).toMatch(/query or fragment/)
  expect(validateBaseURL('https://example.com/v1?')).toMatch(/query or fragment/)
  expect(validateBaseURL('https://example.com/v1#')).toMatch(/query or fragment/)
  expect(validateBaseURL('ftp://example.com')).toMatch(/http or https/)
  expect(validateBaseURL('not a url')).toMatch(/complete http/)
})

it('validates timeouts and embedding dimensions', () => {
  expect(validateTimeout('60')).toBe('')
  expect(validateTimeout('0')).toMatch(/between 1 and 600/)
  expect(validateTimeout('601')).toMatch(/between 1 and 600/)
  expect(validateTimeout('1.5')).toMatch(/whole number/)
  expect(validateDimensions('1024', false)).toBe('')
  expect(validateDimensions('1025', false)).toMatch(/need truncatable/)
  expect(validateDimensions('65536', true)).toBe('')
  expect(validateDimensions('65537', true)).toMatch(/at most 65536/)
})
