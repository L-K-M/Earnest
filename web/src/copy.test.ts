import {expect, it} from 'vitest'
import {emptyCopy} from './copy'
it('offers a calm empty state', () => { expect(emptyCopy()).toBe('Nothing new. A rare and beautiful thing.') })
