import { reactive } from 'vue'
import { toast } from '../stores/app'

export const designer = reactive({
  open: false,
  mode: 'create' as 'create' | 'design',
  connId: '',
  table: '',
  nonce: 0,
})

export function openCreateTable(connId: string) {
  if (!connId) {
    toast('请先连接', 'error')
    return
  }
  designer.mode = 'create'
  designer.connId = connId
  designer.table = ''
  designer.nonce++
  designer.open = true
}

export function openDesignTable(connId: string, table: string) {
  if (!connId) {
    toast('请先连接', 'error')
    return
  }
  designer.mode = 'design'
  designer.connId = connId
  designer.table = table
  designer.nonce++
  designer.open = true
}

export function closeDesigner() {
  designer.open = false
}
