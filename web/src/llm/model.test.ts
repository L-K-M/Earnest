import {expect, it} from 'vitest'
import {apiKeyStatus, capabilityFor, draftFromDiscovered, sameDiscoveryProvider, taskCapability, validateBaseURL, validateDimensions, validateDiscoveredModel, validateMoney, validateTimeout, type ProviderDraft} from './model'

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

const draftProvider = (patch: Partial<ProviderDraft>): ProviderDraft => ({
  name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true,
  monthly_limit_usd: '0', timeout_seconds: '60', token_field: 'max_tokens',
  api_key: '', clear_api_key: false, original_name: 'openai', ...patch,
})

it('summarizes the draft key status over saved, entered, removal and none', () => {
  expect(apiKeyStatus(draftProvider({}))).toBe('saved')
  expect(apiKeyStatus(draftProvider({api_key: '  sk-typed  '}))).toBe('entered')
  expect(apiKeyStatus(draftProvider({clear_api_key: true}))).toBe('removal')
  expect(apiKeyStatus(draftProvider({has_api_key: false}))).toBe('none')
  expect(apiKeyStatus(draftProvider({api_key: 'sk-x', clear_api_key: true}))).toBe('entered')
})

it('compares exact provider drafts including colliding key hashes', () => {
  const base = draftProvider({})
  expect(sameDiscoveryProvider(base, draftProvider({}))).toBe(true)
  expect(sameDiscoveryProvider(base, draftProvider({base_url: 'https://other.example/v1'}))).toBe(false)
  expect(sameDiscoveryProvider(draftProvider({api_key: 'Aa'}), draftProvider({api_key: 'BB'}))).toBe(false)
})

it('maps task routes to capabilities and reads catalogue flags honestly', () => {
  expect(taskCapability('embed')).toBe('embedding')
  expect(taskCapability('ask')).toBe('chat')
  expect(capabilityFor({id: 'a', name: 'a'}, 'chat')).toBeUndefined()
  expect(capabilityFor({id: 'a', name: 'a', chat: false}, 'chat')).toBe(false)
  expect(capabilityFor({id: 'a', name: 'a', embedding: true}, 'embedding')).toBe(true)
})

it('prefills review drafts only where the catalogue speaks', () => {
  const chatOnly = draftFromDiscovered({id: 'chat', name: 'Chat', chat: true, embedding: false, input_per_1m: '2', output_per_1m: '6'})
  expect(chatOnly).toMatchObject({name: 'chat', input_per_1m: '2', output_per_1m: '6', embedding_per_1m: '0', dimensions: '0', truncatable: false})
  const embedOnly = draftFromDiscovered({id: 'emb', name: 'Emb', chat: false, embedding: true})
  expect(embedOnly).toMatchObject({input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '', dimensions: ''})
  const unknown = draftFromDiscovered({id: 'unk', name: 'Unk'})
  expect(unknown).toMatchObject({input_per_1m: '', output_per_1m: '', embedding_per_1m: '', dimensions: ''})
})

it('requires real geometry from a model the provider says can embed', () => {
  const embedder = {id: 'emb', name: 'Emb', embedding: true}
  expect(validateDiscoveredModel({name: 'emb', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.1', dimensions: '0', truncatable: false}, embedder, new Set())).toEqual([expect.stringMatching(/dimensions are required/)])
  expect(validateDiscoveredModel({name: 'emb', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.1', dimensions: '512', truncatable: false}, embedder, new Set())).toHaveLength(0)
  const chatter = {id: 'chat', name: 'Chat', chat: true, embedding: false}
  expect(validateDiscoveredModel({name: 'chat', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: '0', truncatable: false}, chatter, new Set())).toHaveLength(0)
})
