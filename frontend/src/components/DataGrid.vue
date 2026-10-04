<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  columns: string[]
  rows: any[][]
  sortColumn?: string
  sortAsc?: boolean
  clickable?: boolean
  selectedRow?: number
  loading?: boolean
}>()

const emit = defineEmits<{
  (e: 'sort', col: string): void
  (e: 'rowClick', index: number): void
}>()

const cellText = (v: any): string => {
  if (v === null || v === undefined) return 'NULL'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

const cellClass = (v: any) => ({
  'cell-null': v === null || v === undefined,
  'cell-json': v !== null && typeof v === 'object',
  'cell-num': typeof v === 'number',
})
</script>

<template>
  <div class="grid-wrap">
    <table class="grid">
      <thead>
        <tr>
          <th v-for="c in columns" :key="c" @click="emit('sort', c)">
            {{ c }}
            <span v-if="sortColumn === c" class="sort-arrow">{{ sortAsc ? '▲' : '▼' }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(row, i) in rows"
          :key="i"
          :class="{ clickable, selected: selectedRow === i }"
          @click="emit('rowClick', i)"
        >
          <td v-for="(c, j) in columns" :key="j" :class="cellClass(row[j])" :title="cellText(row[j])">
            {{ cellText(row[j]) }}
          </td>
        </tr>
        <tr v-if="rows.length === 0 && !loading">
          <td :colspan="columns.length || 1" class="no-rows">没有数据</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.no-rows {
  text-align: center;
  color: var(--text-faint);
  padding: 28px !important;
  font-family: var(--sans) !important;
}
th { cursor: pointer; }
th:hover { color: var(--text); }
</style>
