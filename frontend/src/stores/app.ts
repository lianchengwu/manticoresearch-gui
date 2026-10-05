import { computed, inject, provide, reactive, type InjectionKey, type Ref } from 'vue'
import { ConnectionService, TableService, errText } from '../lib/api'
import type { Connection, TableInfo } from '../lib/types'

export interface Tab {
  id: string
  kind: 'sql' | 'table'
  table?: string
  view: 'data' | 'schema'
}

export interface Session {
  connId: string
  connected: boolean
  connecting: boolean
  version: string
  via: string
  tables: TableInfo[]
  tablesLoading: boolean
  tabs: Tab[]
  activeTabId: string
}

export interface Toast {
  id: number
  type: 'info' | 'success' | 'error'
  text: string
}

interface ConfirmState {
  open: boolean
  title: string
  body: string
  danger: boolean
  resolve: ((v: boolean) => void) | null
}



function emptySession(id: string): Session {
  return {
    connId: id,
    connected: false,
    connecting: false,
    version: '',
    via: '',
    tables: [],
    tablesLoading: false,
    tabs: [{ id: 'sql', kind: 'sql', view: 'data' }],
    activeTabId: 'sql',
  }
}

export const store = reactive({
  connections: [] as Connection[],
  sessions: {} as Record<string, Session>,
  openIds: [] as string[],
  activeId: '',
  toasts: [] as Toast[],
  modal: {
    connection: false,
    editConnectionId: '',
  },
  confirm: {
    open: false,
    title: '确认操作',
    body: '',
    danger: true,
    resolve: null as ((v: boolean) => void) | null,
  } as ConfirmState,
})

export const SessionIdKey: InjectionKey<Ref<string>> = Symbol('sessionId')

export function provideSessionId(id: Ref<string>) {
  provide(SessionIdKey, id)
}

export function useSessionId(): Ref<string> {
  return inject(SessionIdKey, computed(() => store.activeId))
}

export function useSession(): Ref<Session | undefined> {
  const id = useSessionId()
  return computed(() => store.sessions[id.value])
}

export const activeConn = () => store.connections.find((c) => c.id === store.activeId) ?? null

export function connById(id: string) {
  return store.connections.find((c) => c.id === id) ?? null
}

export function sessionOf(id: string): Session | undefined {
  return store.sessions[id]
}

let toastSeq = 1
export function toast(text: string, type: Toast['type'] = 'info') {
  const id = toastSeq++
  store.toasts.push({ id, type, text })
  setTimeout(() => {
    const i = store.toasts.findIndex((t) => t.id === id)
    if (i >= 0) store.toasts.splice(i, 1)
  }, 4200)
}

export function confirmBox(body: string, title = '确认操作', danger = true): Promise<boolean> {
  return new Promise((resolve) => {
    store.confirm.open = true
    store.confirm.title = title
    store.confirm.body = body
    store.confirm.danger = danger
    store.confirm.resolve = resolve
  })
}

export function resolveConfirm(v: boolean) {
  store.confirm.open = false
  store.confirm.resolve?.(v)
  store.confirm.resolve = null
}

function persistOpen() {
  localStorage.setItem('msgui.open', JSON.stringify(store.openIds))
  localStorage.setItem('msgui.active', store.activeId)
}

function readSavedOpen(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem('msgui.open') ?? '[]')
    if (Array.isArray(raw)) return raw.filter((x) => typeof x === 'string')
  } catch {
    /* ignore */
  }
  const last = localStorage.getItem('msgui.active')
  return last ? [last] : []
}

function ensureSession(id: string): Session {
  if (!store.sessions[id]) {
    store.sessions[id] = emptySession(id)
  }
  if (!store.openIds.includes(id)) {
    store.openIds.push(id)
  }
  return store.sessions[id]
}

function dropSession(id: string) {
  const i = store.openIds.indexOf(id)
  if (i >= 0) store.openIds.splice(i, 1)
  delete store.sessions[id]
  if (store.activeId === id) {
    store.activeId = store.openIds[store.openIds.length - 1] ?? ''
  }
}

export function closeSession(id: string) {
  dropSession(id)
  persistOpen()
}

export function focusSession(id: string) {
  if (!store.sessions[id]) return
  store.activeId = id
  persistOpen()
}

export async function loadConnections() {
  try {
    store.connections = (await ConnectionService.ListConnections()) ?? []
    const known = new Set(store.connections.map((c) => c.id))
    for (const id of [...store.openIds]) {
      if (!known.has(id)) dropSession(id)
    }
    if (store.openIds.length === 0 && store.connections.length > 0) {
      const saved = readSavedOpen().filter((id) => known.has(id))
      const ids = saved.length > 0 ? saved : [store.connections[0].id]
      for (const id of ids) {
        await activate(id, { silent: true })
      }
      persistOpen()
    }
  } catch (e) {
    toast(errText(e), 'error')
  }
}

export async function activate(id: string, opts: { silent?: boolean; reconnect?: boolean } = {}) {
  const conn = store.connections.find((c) => c.id === id)
  if (!conn) return
  const existed = !!store.sessions[id]
  const sess = ensureSession(id)
  store.activeId = id
  persistOpen()
  if (existed && sess.connected && !opts.reconnect && !sess.connecting) {
    return
  }
  sess.connected = false
  sess.version = ''
  sess.via = ''
  sess.tables = []
  sess.connecting = true
  if (!existed) {
    sess.tabs = [{ id: 'sql', kind: 'sql', view: 'data' }]
    sess.activeTabId = 'sql'
  }

  const [testRes, tablesRes] = await Promise.allSettled([
    ConnectionService.TestConnection(conn),
    TableService.ListTables(id),
  ])
  if (!store.sessions[id]) return
  sess.connecting = false
  if (testRes.status === 'fulfilled' && testRes.value) {
    sess.connected = true
    sess.version = testRes.value.version
    sess.via = testRes.value.via
  } else if (testRes.status === 'rejected') {
    toast(`连接失败:${errText(testRes.reason)}`, 'error')
  }
  if (tablesRes.status === 'fulfilled') {
    sess.tables = tablesRes.value ?? []
  } else {
    sess.tables = []
    if (sess.connected) toast(`获取表列表失败:${errText(tablesRes.reason)}`, 'error')
  }
}

export async function refreshTables(connId = store.activeId) {
  const sess = store.sessions[connId]
  if (!sess) return
  sess.tablesLoading = true
  try {
    sess.tables = (await TableService.ListTables(connId)) ?? []
  } catch (e) {
    toast(errText(e), 'error')
  } finally {
    sess.tablesLoading = false
  }
}

export const tableTick = reactive({ n: 0, table: '' })

export function bumpTable(table = '') {
  tableTick.n++
  tableTick.table = table
}

export function openTableTab(table: string, connId = store.activeId, view?: Tab['view']) {
  const sess = store.sessions[connId]
  if (!sess) return
  const id = 'table:' + table
  let tab = sess.tabs.find((t) => t.id === id)
  if (!tab) {
    tab = { id, kind: 'table', table, view: view ?? 'data' }
    sess.tabs.push(tab)
  } else if (view) {
    tab.view = view
  }
  sess.activeTabId = id
  store.activeId = connId
}

export function retargetTableTab(oldName: string, newName: string, connId = store.activeId) {
  const sess = store.sessions[connId]
  if (!sess || !newName || oldName === newName) return
  const oldId = 'table:' + oldName
  const newId = 'table:' + newName
  const tab = sess.tabs.find((t) => t.id === oldId)
  if (!tab) return
  if (sess.tabs.some((t) => t.id === newId)) {
    closeTab(oldId, connId)
    return
  }
  tab.id = newId
  tab.table = newName
  if (sess.activeTabId === oldId) sess.activeTabId = newId
}

export function closeTab(id: string, connId = store.activeId) {
  const sess = store.sessions[connId]
  if (!sess) return
  const i = sess.tabs.findIndex((t) => t.id === id)
  if (i < 0) return
  sess.tabs.splice(i, 1)
  if (sess.activeTabId === id) {
    const next = sess.tabs[Math.min(i, sess.tabs.length - 1)]
    sess.activeTabId = next ? next.id : 'sql'
    if (!next) sess.tabs.push({ id: 'sql', kind: 'sql', view: 'data' })
  }
}

export function openConnectionModal(editId = '') {
  store.modal.editConnectionId = editId
  store.modal.connection = true
}
