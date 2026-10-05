import { reactive } from 'vue'
import { QueryService, TableService, errText } from './api'
import { splitStatements } from './schema'
import type { QueryResult } from './types'
import {
  bumpTable,
  closeTab,
  confirmBox,
  openTableTab,
  refreshTables,
  retargetTableTab,
  toast,
} from '../stores/app'
import { closeDesigner, designer, openDesignTable } from '../components/tabledesigner'

export interface TableAction {
  id: string
  label: string
  danger?: boolean
  sep?: boolean
}

export const tableActions: TableAction[] = [
  { id: 'data', label: '浏览数据' },
  { id: 'design', label: '设计结构' },
  { id: 'copy-name', label: '复制表名' },
  { id: 'copy-create', label: '复制建表语句' },
  { id: 'sep1', label: '', sep: true },
  { id: 'status', label: '查看状态' },
  { id: 'settings', label: '查看设置' },
  { id: 'optimize', label: '优化磁盘块' },
  { id: 'flush-ram', label: '刷出内存块' },
  { id: 'flush', label: '强制落盘' },
  { id: 'sep2', label: '', sep: true },
  { id: 'like', label: '复制结构…' },
  { id: 'like-data', label: '复制结构及数据…' },
  { id: 'rename', label: '重命名…' },
  { id: 'truncate', label: '清空文档', danger: true },
  { id: 'drop', label: '删除表', danger: true },
]

export const ops = reactive({
  resultOpen: false,
  resultTitle: '',
  result: null as QueryResult | null,
  promptOpen: false,
  promptTitle: '',
  promptLabel: '',
  promptValue: '',
  promptHint: '',
  promptResolve: null as ((v: string | null) => void) | null,
})

export function askName(title: string, label: string, initial = '', hint = ''): Promise<string | null> {
  return new Promise((resolve) => {
    ops.promptTitle = title
    ops.promptLabel = label
    ops.promptValue = initial
    ops.promptHint = hint
    ops.promptResolve = resolve
    ops.promptOpen = true
  })
}

export function resolvePrompt(ok: boolean) {
  const raw = ops.promptValue.trim()
  ops.promptOpen = false
  const resolve = ops.promptResolve
  ops.promptResolve = null
  resolve?.(ok && raw ? raw : null)
}

export function showResult(title: string, res: QueryResult | null) {
  ops.resultTitle = title
  ops.result = res ?? { columns: [], rows: [], total: null, tookMs: 0, message: '', error: '空结果' }
  ops.resultOpen = true
}

export function closeResult() {
  ops.resultOpen = false
}

function failed(res: QueryResult | null): boolean {
  if (!res?.error) return false
  toast(res.error, 'error')
  return true
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制', 'success')
  } catch (e) {
    toast(errText(e) || '复制失败', 'error')
  }
}

function createSQL(res: QueryResult | null): string {
  const row = res?.rows?.[0]
  if (!row || row.length === 0) return ''
  return String(row[row.length - 1] ?? '')
}

export async function execStatements(connId: string, sql: string): Promise<boolean> {
  const stmts = splitStatements(sql)
  if (stmts.length === 0) {
    toast('SQL 为空', 'error')
    return false
  }
  for (const stmt of stmts) {
    try {
      const res = await QueryService.ExecuteSQL(connId, stmt)
      if (failed(res)) return false
    } catch (e) {
      toast(errText(e), 'error')
      return false
    }
  }
  return true
}

export async function truncateTable(connId: string, table: string): Promise<boolean> {
  const ok = await confirmBox(`清空表 ${table} 的全部文档?此操作不可撤销。`, 'TRUNCATE TABLE')
  if (!ok) return false
  try {
    const res = await TableService.TruncateTable(connId, table)
    if (failed(res)) return false
    toast('表已清空', 'success')
    refreshTables(connId)
    bumpTable(table)
    return true
  } catch (e) {
    toast(errText(e), 'error')
    return false
  }
}

export async function dropTable(connId: string, table: string): Promise<boolean> {
  const ok = await confirmBox(`删除表 ${table}?此操作不可撤销。`, 'DROP TABLE')
  if (!ok) return false
  try {
    const res = await TableService.DropTable(connId, table)
    if (failed(res)) return false
    toast('表已删除', 'success')
    if (designer.open && designer.table === table) closeDesigner()
    closeTab('table:' + table, connId)
    refreshTables(connId)
    return true
  } catch (e) {
    toast(errText(e), 'error')
    return false
  }
}

async function showQuery(connId: string, title: string, run: () => Promise<QueryResult | null>) {
  try {
    const res = await run()
    if (res?.error) toast(res.error, 'error')
    showResult(title, res)
  } catch (e) {
    toast(errText(e), 'error')
  }
}

export async function runTableAction(connId: string, table: string, id: string) {
  if (!connId) {
    toast('请先连接', 'error')
    return
  }
  switch (id) {
    case 'data':
      openTableTab(table, connId, 'data')
      return
    case 'design':
      openTableTab(table, connId, 'schema')
      openDesignTable(connId, table)
      return
    case 'copy-name':
      await copyText(table)
      return
    case 'copy-create': {
      try {
        const res = await TableService.ShowCreateTable(connId, table)
        if (failed(res)) return
        const sql = createSQL(res)
        if (!sql) {
          toast('没有建表语句', 'error')
          return
        }
        await copyText(sql)
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'status':
      await showQuery(connId, `状态 · ${table}`, () => TableService.ShowTableStatus(connId, table))
      return
    case 'settings':
      await showQuery(connId, `设置 · ${table}`, () => TableService.ShowTableSettings(connId, table))
      return
    case 'optimize': {
      const ok = await confirmBox(`优化表 ${table} 的磁盘块?可能消耗较多 IO。`, 'OPTIMIZE TABLE', false)
      if (!ok) return
      try {
        const res = await TableService.OptimizeTable(connId, table)
        if (failed(res)) return
        toast(res?.message || '已提交优化', 'success')
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'flush-ram': {
      const ok = await confirmBox(`把 ${table} 的内存块刷成新的磁盘块?`, 'FLUSH RAMCHUNK', false)
      if (!ok) return
      try {
        const res = await TableService.FlushRamchunk(connId, table)
        if (failed(res)) return
        toast(res?.message || '已刷出内存块', 'success')
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'flush': {
      const ok = await confirmBox(`把 ${table} 的内存块强制落盘?`, 'FLUSH TABLE', false)
      if (!ok) return
      try {
        const res = await TableService.FlushTable(connId, table)
        if (failed(res)) return
        toast(res?.message || '已落盘', 'success')
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'like':
    case 'like-data': {
      const withData = id === 'like-data'
      const name = await askName(
        withData ? '复制结构及数据' : '复制结构',
        '新表名',
        `${table}_copy`,
        withData ? 'CREATE TABLE … LIKE … WITH DATA' : 'CREATE TABLE … LIKE …',
      )
      if (!name) return
      try {
        const res = await TableService.CreateTableLike(connId, name, table, withData)
        if (failed(res)) return
        toast(withData ? '已复制表' : '已复制结构', 'success')
        await refreshTables(connId)
        openTableTab(name, connId, 'schema')
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'rename': {
      const name = await askName('重命名表', '新表名', table, 'ALTER TABLE … RENAME，需要 Manticore Buddy')
      if (!name || name === table) return
      try {
        const res = await TableService.RenameTable(connId, table, name)
        if (failed(res)) return
        toast('已重命名', 'success')
        if (designer.open && designer.table === table) {
          designer.table = name
          designer.nonce++
        }
        retargetTableTab(table, name, connId)
        refreshTables(connId)
      } catch (e) {
        toast(errText(e), 'error')
      }
      return
    }
    case 'truncate':
      await truncateTable(connId, table)
      return
    case 'drop':
      await dropTable(connId, table)
      return
    default:
      return
  }
}
