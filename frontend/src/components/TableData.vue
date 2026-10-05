<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { tableTick, toast, confirmBox, useSessionId } from '../stores/app'
import { TableService, errText } from '../lib/api'
import { dropTable, truncateTable } from '../lib/tableops'
import type { QueryResult } from '../lib/types'
import DataGrid from './DataGrid.vue'
import TableActionMenu from './TableActionMenu.vue'
import { openDocModal } from './docmodal'

const props = defineProps<{ table: string }>()
const connId = useSessionId()


const result = ref<QueryResult | null>(null)
const loading = ref(false)
const query = ref('')
const appliedQuery = ref('')
const page = ref(0)
const pageSize = ref(50)
const sortBy = ref('')
const sortAsc = ref(false)
const selectedRow = ref(-1)
const detailOpen = ref(false)

const total = computed(() => result.value?.total ?? null)
const pageCount = computed(() => (total.value !== null ? Math.max(1, Math.ceil(total.value / pageSize.value)) : null))

async function load() {
  if (!connId.value) return
  loading.value = true
  selectedRow.value = -1
  detailOpen.value = false
  try {
    result.value = await TableService.BrowseDocuments(connId.value, {
      table: props.table,
      query: appliedQuery.value,
      page: page.value,
      pageSize: pageSize.value,
      sortBy: sortBy.value,
      sortAsc: sortAsc.value,
    })
    if (result.value?.error) toast(result.value.error, 'error')
  } catch (e) {
    result.value = null
    toast(errText(e), 'error')
  } finally {
    loading.value = false
  }
}

function applySearch() {
  appliedQuery.value = query.value.trim()
  page.value = 0
  load()
}

function onSort(col: string) {
  if (sortBy.value === col) {
    if (sortAsc.value) {
      sortBy.value = ''
      sortAsc.value = false
    } else {
      sortAsc.value = true
    }
  } else {
    sortBy.value = col
    sortAsc.value = false
  }
  page.value = 0
  load()
}

function prev() { if (page.value > 0) { page.value--; load() } }
function next() {
  if (pageCount.value === null || page.value < pageCount.value - 1) { page.value++; load() }
}

const rowObj = computed(() => {
  if (!result.value || selectedRow.value < 0) return null
  const row = result.value.rows[selectedRow.value]
  const obj: Record<string, any> = {}
  result.value.columns.forEach((c, i) => (obj[c] = row[i]))
  return obj
})

const docId = computed(() => (rowObj.value ? String(rowObj.value['id'] ?? '') : ''))
const docJson = computed(() => {
  if (!rowObj.value) return ''
  const clone: Record<string, any> = {}
  for (const [k, v] of Object.entries(rowObj.value)) {
    if (k === 'id' || k === '_score') continue
    clone[k] = v
  }
  return JSON.stringify(clone, null, 2)
})

function openRow(i: number) {
  selectedRow.value = i
  detailOpen.value = true
}

function editSelected() {
  if (!rowObj.value) return
  openDocModal({
    mode: 'replace',
    table: props.table,
    id: docId.value,
    docText: docJson.value,
    connId: connId.value,
    onDone: load,
  })
}

async function deleteSelected() {
  if (!docId.value) return
  const ok = await confirmBox(`删除文档 id=${docId.value}?此操作不可撤销。`, '删除文档')
  if (!ok) return
  try {
    const res = await TableService.DeleteDocument(connId.value, props.table, docId.value)
    toast(res?.message ?? '已删除', 'success')
    detailOpen.value = false
    load()
  } catch (e) {
    toast(errText(e), 'error')
  }
}

async function truncate() {
  if (await truncateTable(connId.value, props.table)) {
    page.value = 0
    load()
  }
}

async function drop() {
  await dropTable(connId.value, props.table)
}

watch(() => props.table, load)
watch(() => tableTick.n, () => {
  if (tableTick.table && tableTick.table !== props.table) return
  page.value = 0
  load()
})
onMounted(load)
</script>

<template>
  <div class="browser">
    <div class="toolbar">
      <form class="search" @submit.prevent="applySearch">
        <input v-model="query" placeholder='全文检索 (query_string 语法,如 "hello world")' />
        <button class="btn sm" type="submit">搜索</button>
        <button v-if="appliedQuery" class="btn ghost sm" type="button" @click="query = ''; applySearch()">清除</button>
      </form>
      <span class="flex1"></span>
      <button class="btn sm" @click="openDocModal({ mode: 'insert', table, docText: '{}', connId: connId, onDone: load })">＋ 插入文档</button>
      <TableActionMenu :table="table" />
      <button class="btn ghost sm" title="刷新" @click="load()">⟳</button>
      <button class="btn danger sm" title="清空表" @click="truncate">清空</button>
      <button class="btn danger sm" title="删除表" @click="drop">删表</button>
    </div>

    <div class="grid-area">
      <div v-if="result?.error" class="pad"><div class="error-banner">{{ result.error }}</div></div>
      <DataGrid
        v-else
        :columns="result?.columns ?? []"
        :rows="result?.rows ?? []"
        :sort-column="sortBy"
        :sort-asc="sortAsc"
        clickable
        :selected-row="selectedRow"
        :loading="loading"
        @sort="onSort"
        @row-click="openRow"
      />
      <div v-if="loading" class="loading-mask"><span class="spinner"></span></div>
    </div>

    <div class="pagination">
      <span class="dim">
        {{ total !== null ? `共 ${total.toLocaleString()} 条` : '' }}
        <template v-if="pageCount !== null"> · 第 {{ page + 1 }} / {{ pageCount }} 页</template>
      </span>
      <span class="flex1"></span>
      <label class="dim" style="display: inline-flex; align-items: center; gap: 6px">
        每页
        <select v-model.number="pageSize" @change="page = 0; load()">
          <option :value="25">25</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
          <option :value="200">200</option>
        </select>
      </label>
      <button class="btn sm" :disabled="page === 0 || loading" @click="prev">‹ 上一页</button>
      <button class="btn sm" :disabled="(pageCount !== null && page >= pageCount - 1) || loading" @click="next">下一页 ›</button>
    </div>

    <!-- document detail drawer -->
    <div v-if="detailOpen && rowObj" class="drawer-overlay" @click="detailOpen = false"></div>
    <aside v-if="detailOpen && rowObj" class="drawer">
      <div class="drawer-head">
        <span class="doc-id mono">id = {{ docId }}</span>
        <span class="flex1"></span>
        <button class="btn sm primary" @click="editSelected">编辑</button>
        <button class="btn sm danger" @click="deleteSelected">删除</button>
        <button class="modal-close" @click="detailOpen = false">×</button>
      </div>
      <pre class="doc-json">{{ docJson }}</pre>
    </aside>
  </div>
</template>

<style scoped>
.browser { flex: 1; display: flex; flex-direction: column; min-height: 0; }
.toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.search { display: flex; gap: 6px; flex: 1; max-width: 480px; }
.search input { flex: 1; }
.flex1 { flex: 1; }
.pad { padding: 10px 12px; }
.grid-area { flex: 1; position: relative; display: flex; flex-direction: column; min-height: 0; }
.loading-mask {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(13, 15, 22, 0.45);
  z-index: 5;
}
.pagination {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  border-top: 1px solid var(--border-soft);
  flex: none;
  font-size: 12px;
}
.dim { color: var(--text-dim); }

.drawer-overlay { position: fixed; inset: 0; background: var(--overlay); z-index: 50; }
.drawer {
  position: fixed;
  top: var(--titlebar-h);
  right: 0;
  bottom: var(--statusbar-h);
  width: 440px;
  background: var(--panel);
  border-left: 1px solid var(--border);
  box-shadow: -12px 0 40px rgba(0, 0, 0, 0.35);
  z-index: 51;
  display: flex;
  flex-direction: column;
  animation: drawer-in 0.15s ease-out;
}
@keyframes drawer-in {
  from { transform: translateX(30px); opacity: 0; }
  to { transform: none; opacity: 1; }
}
.drawer-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.doc-id { color: var(--accent); font-weight: 600; font-size: 12.5px; }
.doc-json {
  flex: 1;
  overflow: auto;
  margin: 0;
  padding: 12px;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.6;
  color: var(--text);
  user-select: text;
  white-space: pre-wrap;
  word-break: break-all;
}
.modal-close {
  background: none; border: none; color: var(--text-faint);
  font-size: 18px; cursor: pointer; padding: 2px 8px; border-radius: 4px;
}
.modal-close:hover { color: var(--text); background: var(--panel-3); }
</style>
