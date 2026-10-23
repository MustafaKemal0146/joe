import { describe, expect, it } from 'vitest'
import { parseMention, shown } from './ask'
import type { PersonaInfo } from './types'

const all = [{ id: 'jung', name: 'Carl Gustav Jung' }, { id: 'klein', name: 'Melanie Klein' }] as PersonaInfo[]

describe('parseMention', () => {
  it('@kimlik ile tek mercek seçer ve soruyu ayırır', () => {
    const r = parseMention('@jung bu ilişki nasıl okunur?', all)
    expect(r.persona?.id).toBe('jung')
    expect(r.question).toBe('bu ilişki nasıl okunur?')
  })
  it('soyadıyla da eşleşir', () => {
    expect(parseMention('@Klein ne değişti', all).persona?.id).toBe('klein')
  })
  it('bilinmeyen anma soruda kalır', () => {
    const r = parseMention('@kimse deneme', all)
    expect(r.persona).toBeUndefined()
    expect(r.question).toBe('@kimse deneme')
  })
})

describe('shown', () => {
  it('takip sorusunda yalnızca yeni soruyu gösterir', () => {
    expect(shown('Önceki soru: a\n\nYeni soru: b')).toBe('b')
    expect(shown('düz soru')).toBe('düz soru')
    expect(shown(undefined)).toBe('')
  })
})
