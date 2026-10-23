import type { PersonaInfo } from './types'

const MARK = '\n\nYeni soru: '
/** Takip sorusunda sunucu önceki özeti sorunun başına ekler; kullanıcıya yalnızca kendi sorusu gösterilir. */
export const shown = (q?: string) => { const i = q?.lastIndexOf(MARK) ?? -1; return i >= 0 ? q!.slice(i + MARK.length) : (q ?? '') }

/** "@jung nasıl okur?" → persona kimliği ve soru metni. */
export function parseMention(text: string, all: PersonaInfo[]): { persona?: PersonaInfo; question: string } {
  const m = text.match(/^\s*@([\p{L}\d_-]+)\s*/u)
  if (!m) return { question: text.trim() }
  const key = m[1]!.toLocaleLowerCase('tr-TR')
  const persona = all.find((p) => p.id.toLowerCase() === key || p.name.toLocaleLowerCase('tr-TR').split(/\s+/).pop() === key)
  return persona ? { persona, question: text.slice(m[0].length).trim() } : { question: text.trim() }
}

