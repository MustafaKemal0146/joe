import { describe, expect, it } from 'vitest'
import { maybeSame, partnerFromFile } from './Onboarding'
import type { Person } from '@/lib/types'

const person = (name: string, ...ids: string[]) => ({ id: 1, name, note: '', is_self: false, created_at: '', identities: ids.map((v, i) => ({ id: i, value: v })) }) as unknown as Person

describe('partnerFromFile', () => {
  it('WhatsApp dosya adından karşı tarafı çıkarır', () => {
    expect(partnerFromFile('WhatsApp Sohbeti - Ayşe Yılmaz.txt')).toBe('Ayşe Yılmaz')
    expect(partnerFromFile('rastgele.txt')).toBe('')
  })
})

describe('maybeSame', () => {
  it('ad ve kullanıcı adı benzerliğini yakalar, ilgisizi yakalamaz', () => {
    expect(maybeSame('Ayşe', person('Ayşe Kaya'))).toBe(true)
    expect(maybeSame('Ayşe', person('Birisi', 'ayse_k'))).toBe(true)
    expect(maybeSame('Mehmet', person('Ayşe Kaya', 'ayse_k'))).toBe(false)
    expect(maybeSame('Al', person('Ali'))).toBe(false) // çok kısa ad eşleştirilmez
  })
})
