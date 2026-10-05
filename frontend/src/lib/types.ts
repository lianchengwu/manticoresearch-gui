export interface Hop {
  type: 'http' | 'socks5' | 'ssh'
  host: string
  port: number
  username: string
  password: string
  disabled: boolean
  // ssh only
  authType: 'password' | 'key'
  keyPath: string
  keyPassphrase: string
  remoteHost: string
  remotePort: number
}

export interface NodeAddr {
  scheme: string
  host: string
  port: number
}

export interface Connection {
  id: string
  name: string
  scheme: string
  host: string
  port: number
  username: string
  password: string
  nodes: NodeAddr[] // non-empty = cluster mode
  hops: Hop[] // ordered network chain
}

export interface QueryResult {
  columns: string[]
  rows: any[][]
  total: number | null
  tookMs: number
  message: string
  error: string
}

export interface TableInfo {
  name: string
  type: string
  docs?: string
}

export interface BrowseOptions {
  table: string
  query: string
  page: number
  pageSize: number
  sortBy: string
  sortAsc: boolean
}

export interface MutationResult {
  message: string
}
export interface ColumnDef {
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

export interface OptionPair {
  name: string
  value: string
}

export interface DistMember {
  kind: 'local' | 'agent' | string
  value: string
}

export interface CreateTableSpec {
  name: string
  ifNotExists: boolean
  kind: string
  columns: ColumnDef[]
  options: OptionPair[]
  members: DistMember[]
  engine: string
}

export interface SchemaChange {
  action: 'add' | 'drop' | 'modify' | 'setting' | 'rename' | string
  column: ColumnDef
  newName: string
  settings: OptionPair[]
}

export interface SQLPreview {
  sql: string
  sqls: string[]
  error: string
}

export interface SchemaApplyResult {
  applied: number
  total: number
  error: string
  failedSql: string
  message: string
}

export interface TestResult {
  version: string
  via: string
  tookMs: number
}

export function emptyNode(): NodeAddr {
  return { scheme: 'http', host: '127.0.0.1', port: 9308 }
}

export function emptyHop(type: Hop['type']): Hop {
  return {
    type,
    host: '127.0.0.1',
    port: type === 'ssh' ? 22 : type === 'http' ? 8080 : 1080,
    username: '',
    password: '',
    disabled: false,
    authType: 'password',
    keyPath: '',
    keyPassphrase: '',
    remoteHost: '',
    remotePort: 0,
  }
}

export function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v))
}
