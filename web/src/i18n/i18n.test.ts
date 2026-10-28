import { describe, expect, it } from 'vitest'
import zhHans from './zh-Hans'
import en from './en'

type MessageTree = Record<string, unknown>

function flatten(obj: MessageTree, prefix = ''): Map<string, string> {
  const out = new Map<string, string>()
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      for (const [k, v] of flatten(value as MessageTree, path)) out.set(k, v)
    } else {
      out.set(path, String(value))
    }
  }
  return out
}

describe('i18n messages', () => {
  const zh = flatten(zhHans as MessageTree)
  const english = flatten(en as MessageTree)

  it('en and zh-Hans have identical key sets', () => {
    expect([...english.keys()].sort()).toEqual([...zh.keys()].sort())
  })

  it('every English message is non-empty and contains no Chinese characters', () => {
    const bad = [...english.entries()].filter(([, v]) => v.trim() === '' || /[\u4e00-\u9fff]/.test(v))
    expect(bad).toEqual([])
  })

  it('placeholder sets match between languages', () => {
    const placeholders = (v: string) => [...v.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort()
    const mismatched = [...english.keys()]
      .filter((k) => zh.has(k))
      .filter((k) => JSON.stringify(placeholders(english.get(k)!)) !== JSON.stringify(placeholders(zh.get(k)!)))
      .map((k) => ({ key: k, en: english.get(k), zh: zh.get(k) }))
    expect(mismatched).toEqual([])
  })
})
