<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { tableTick, toast, useSessionId } from '../stores/app'
import { TableService, errText } from '../lib/api'
import type { QueryResult } from '../lib/types'
import DataGrid from './DataGrid.vue'
import TableActionMenu from './TableActionMenu.vue'
import { openDesignTable } from './tabledesigner'

const props = defineProps<{ table: string }>()
const connId = useSessionId()

const describe = ref<QueryResult | null>(null)
const createSql = ref('')
const loading = ref(false)

async function load() {
  if (!connId.value) return
  loading.value = true
  try {
    const [d, c] = await Promise.all([
      TableService.DescribeTable(connId.value, props.table),
      TableService.ShowCreateTable(connId.value, props.table),
    ])
    describe.value = d
    if (c?.error) toast(c.error, 'error')
    if (c && c.rows.length > 0) {
      createSql.value = String(c.rows[0][c.rows[0].length - 1] ?? '')
    } else {
      createSql.value = ''
    }
  } catch (e) {
    toast(errText(e), 'error')
  } finally {
    loading.value = false
  }
}

watch(() => props.table, load)
watch(() => tableTick.n, () => {
  if (!tableTick.table || tableTick.table === props.table) load()
})
onMounted(load)
</script>

<template>
  <div class="schema">
    <div class="schema-bar">
      <button class="btn sm primary" @click="openDesignTable(connId, table)">设计表</button>
      <button class="btn sm" @click="load">刷新</button>
      <span class="flex1"></span>
      <TableActionMenu :table="table" />
    </div>
    <div class="schema-grid">
      <div class="schema-section">
        <div class="section-label">字段</div>
        <DataGrid :columns="describe?.columns ?? []" :rows="describe?.rows ?? []" :loading="loading" />
      </div>
      <div class="schema-section">
        <div class="section-label">建表语句</div>
        <pre class="create-sql">{{ createSql || '—' }}</pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.schema { flex: 1; display: flex; flex-direction: column; min-height: 0; }
.schema-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.flex1 { flex: 1; }
.schema-grid {
  flex: 1;
  display: grid;
  grid-template-columns: minmax(320px, 1fr) minmax(360px, 1.2fr);
  gap: 0;
  min-height: 0;
}
.schema-section {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}
.schema-section:first-child { border-right: 1px solid var(--border-soft); }
.section-label {
  padding: 10px 14px 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  flex: none;
}
.create-sql {
  flex: 1;
  overflow: auto;
  margin: 0;
  padding: 4px 14px 14px;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.6;
  color: var(--code-fg);
  user-select: text;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
