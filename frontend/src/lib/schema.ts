import type { ColumnDef, OptionPair, QueryResult, SchemaChange } from './types'

export interface ColumnDraft {
  name: string
  type: string
  indexed: boolean
  stored: boolean
  attribute: boolean
  engine: string
  secondaryIndex: boolean
  knnType: string
  knnDims: number
  hnswSimilarity: string
}

export interface ExistingField {
  name: string
  type: string
  properties: string
}

export interface PendingChange {
  id: number
  label: string
  change: SchemaChange
}

export const COLUMN_TYPES: Array<[string, string]> = [
  ['text', 'text 全文'],
  ['string', 'string'],
  ['int', 'int'],
  ['bigint', 'bigint'],
  ['float', 'float'],
  ['bool', 'bool'],
  ['json', 'json'],
  ['multi', 'multi'],
  ['multi64', 'multi64'],
  ['timestamp', 'timestamp'],
  ['float_vector', 'float_vector'],
]

export const FT_OPTIONS: Array<{ name: string; label: string; placeholder: string }> = [
  { name: 'morphology', label: 'morphology', placeholder: 'stem_en' },
  { name: 'charset_table', label: 'charset_table', placeholder: '0..9, english, _' },
  { name: 'min_infix_len', label: 'min_infix_len', placeholder: '2' },
  { name: 'min_prefix_len', label: 'min_prefix_len', placeholder: '3' },
  { name: 'min_word_len', label: 'min_word_len', placeholder: '1' },
  { name: 'index_exact_words', label: 'index_exact_words', placeholder: '1' },
  { name: 'expand_keywords', label: 'expand_keywords', placeholder: '1' },
  { name: 'html_strip', label: 'html_strip', placeholder: '1' },
  { name: 'index_field_lengths', label: 'index_field_lengths', placeholder: '1' },
  { name: 'rt_mem_limit', label: 'rt_mem_limit', placeholder: '128M' },
  { name: 'optimize_cutoff', label: 'optimize_cutoff', placeholder: '4' },
]

export function emptyColumn(name = '', type = 'text'): ColumnDraft {
  return {
    name,
    type,
    indexed: true,
    stored: true,
    attribute: false,
    engine: '',
    secondaryIndex: false,
    knnType: '',
    knnDims: 0,
    hnswSimilarity: '',
  }
}

export function emptyColumnDef(): ColumnDef {
  return {
    name: '',
    type: '',
    indexed: false,
    stored: false,
    attribute: false,
    engine: '',
    secondaryIndex: false,
    knnType: '',
    knnDims: 0,
    hnswSimilarity: '',
  }
}

export function toColumnDef(c: ColumnDraft): ColumnDef {
  return {
    name: c.name.trim(),
    type: c.type,
    indexed: c.indexed,
    stored: c.stored,
    attribute: c.attribute,
    engine: c.engine,
    secondaryIndex: c.secondaryIndex,
    knnType: c.knnType,
    knnDims: Number(c.knnDims) || 0,
    hnswSimilarity: c.hnswSimilarity,
  }
}

export function columnLabel(c: ColumnDraft): string {
  const def = toColumnDef(c)
  const flags = [
    def.type === 'text' && def.indexed ? 'indexed' : '',
    def.type === 'text' && def.stored ? 'stored' : '',
    def.type === 'text' && def.attribute ? 'attribute' : '',
    def.type === 'json' && def.secondaryIndex ? 'secondary_index' : '',
    def.type === 'float_vector' && def.knnDims ? `dims=${def.knnDims}` : '',
    def.engine,
  ].filter(Boolean)
  return `${def.name || '?'} ${def.type}${flags.length ? ' ' + flags.join(' ') : ''}`
}

export function parseDescribe(res: QueryResult | null): ExistingField[] {
  if (!res) return []
  const cols = res.columns.map((c) => c.toLowerCase())
  const nameIdx = Math.max(0, cols.findIndex((c) => c === 'field' || c === 'column'))
  const typeIdx = cols.findIndex((c) => c === 'type')
  const propIdx = cols.findIndex((c) => c === 'properties' || c === 'property')
  return res.rows
    .map((row) => ({
      name: String(row[nameIdx] ?? ''),
      type: typeIdx >= 0 ? String(row[typeIdx] ?? '') : '',
      properties: propIdx >= 0 ? String(row[propIdx] ?? '') : '',
    }))
    .filter((f) => f.name !== '')
}

export function canWiden(type: string): boolean {
  const t = type.toLowerCase()
  return t === 'uint' || t === 'int' || t === 'integer'
}

export function filledOptions(values: Record<string, string>, custom: OptionPair[]): OptionPair[] {
  const out: OptionPair[] = []
  for (const [name, value] of Object.entries(values)) {
    const v = value.trim()
    if (v) out.push({ name, value: v })
  }
  for (const c of custom) {
    if (c.name.trim() || c.value.trim()) out.push({ name: c.name.trim(), value: c.value.trim() })
  }
  return out
}

export function splitStatements(sql: string): string[] {
  const out: string[] = []
  let cur = ''
  let quote = ''
  for (let i = 0; i < sql.length; i++) {
    const ch = sql[i]
    if (quote) {
      cur += ch
      if (ch === quote) {
        if (sql[i + 1] === quote) cur += sql[++i]
        else quote = ''
      }
      continue
    }
    if (ch === "'" || ch === '`') {
      quote = ch
      cur += ch
      continue
    }
    if (ch === ';') {
      const s = cur.trim()
      if (s) out.push(s)
      cur = ''
      continue
    }
    cur += ch
  }
  const tail = cur.trim()
  if (tail) out.push(tail)
  return out
}
