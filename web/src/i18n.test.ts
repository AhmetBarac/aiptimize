import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import sv from './locales/sv.json'

type Tree = { [key: string]: string | Tree }

function flatten(tree: Tree, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return typeof value === 'string' ? [path] : flatten(value, path)
  })
}

describe('locales', () => {
  it('have exactly the same keys', () => {
    const enKeys = flatten(en as Tree).sort()
    const svKeys = flatten(sv as Tree).sort()
    expect(svKeys).toEqual(enKeys)
  })

  it('have no empty translations', () => {
    for (const [locale, tree] of Object.entries({ en, sv })) {
      for (const key of flatten(tree as Tree)) {
        const value = key.split('.').reduce<unknown>((node, part) => {
          if (node && typeof node === 'object') return (node as Record<string, unknown>)[part]
          return undefined
        }, tree)
        expect(typeof value, `${locale}.${key}`).toBe('string')
        expect(String(value).trim(), `${locale}.${key}`).not.toBe('')
      }
    }
  })
})
