import { reactive } from 'vue'

export interface DocModalState {
  open: boolean
  mode: 'insert' | 'replace'
  table: string
  id: string
  idInput: string
  docText: string
  connId: string
  saving: boolean
  error: string
  onDone: (() => void) | null
}

export const docModal = reactive<DocModalState>({
  open: false,
  mode: 'insert',
  table: '',
  id: '',
  idInput: '',
  docText: '{}',
  connId: '',
  saving: false,
  error: '',
  onDone: null,
})

export function openDocModal(opts: {
  mode: 'insert' | 'replace'
  table: string
  id?: string
  docText: string
  connId?: string
  onDone?: () => void
}) {
  docModal.open = true
  docModal.mode = opts.mode
  docModal.table = opts.table
  docModal.id = opts.id ?? ''
  docModal.idInput = ''
  docModal.docText = opts.docText
  docModal.connId = opts.connId ?? ''
  docModal.saving = false
  docModal.error = ''
  docModal.onDone = opts.onDone ?? null
}

export function closeDocModal() {
  docModal.open = false
}
