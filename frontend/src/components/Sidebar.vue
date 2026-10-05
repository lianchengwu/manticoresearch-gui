<script setup lang="ts">
import { computed, ref } from 'vue'
import { openTableTab, refreshTables, useSession, useSessionId } from '../stores/app'
import { openCreateTable } from './tabledesigner'
import TableActionMenu from './TableActionMenu.vue'

const filter = ref('')
const sess = useSession()
const connId = useSessionId()
const menu = ref<{ name: string; x: number; y: number } | null>(null)

const filtered = computed(() => {
  const tables = sess.value?.tables ?? []
  const q = filter.value.trim().toLowerCase()
  if (!q) return tables
  return tables.filter((t) => t.name.toLowerCase().includes(q))
})

function openMenu(e: MouseEvent, name: string) {
  menu.value = {
    name,
    x: Math.min(e.clientX, window.innerWidth - 200),
    y: Math.min(e.clientY, window.innerHeight - 320),
  }
}
</script>

<template>
  <aside class="sidebar">
    <div class="side-head">
      <span class="side-title">表 <em>{{ sess?.tables.length ?? 0 }}</em></span>
      <span class="side-actions">
        <button class="btn ghost sm" title="新建表" @click="openCreateTable(connId)">＋</button>
        <button class="btn ghost sm" title="刷新表列表" @click="refreshTables(connId)">
          <span :class="{ spin: sess?.tablesLoading }">⟳</span>
        </button>
      </span>
    </div>
    <input v-model="filter" class="table-filter" placeholder="过滤表名…" />

    <div class="table-list">
      <div v-if="filtered.length === 0 && !sess?.tablesLoading" class="hint empty">
        {{ (sess?.tables.length ?? 0) === 0 ? '没有表' : '无匹配' }}
      </div>
      <button
        v-for="t in filtered"
        :key="t.name"
        class="table-item"
        :class="{ active: sess?.activeTabId === 'table:' + t.name }"
        :title="t.name"
        @click="openTableTab(t.name, connId)"
        @contextmenu.prevent="openMenu($event, t.name)"
      >
        <svg class="tbl-ic" viewBox="0 0 16 16">
          <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.1" />
          <path d="M1.5 6h13M6 6v7.5" stroke="currentColor" stroke-width="1.1" fill="none" />
        </svg>
        <span class="tbl-name">{{ t.name }}</span>
        <span class="badge" :class="'type-' + (t.type || '').toLowerCase()">{{ t.type || 'table' }}</span>
        <span class="tbl-more" title="表操作" @click.stop="openMenu($event, t.name)">⋯</span>
      </button>
    </div>
    <TableActionMenu
      v-if="menu"
      floating
      :table="menu.name"
      :x="menu.x"
      :y="menu.y"
      @close="menu = null"
    />
  </aside>
</template>

<style scoped>
.sidebar {
  width: var(--sidebar-w);
  flex: none;
  display: flex;
  flex-direction: column;
  background: var(--panel);
  border-right: 1px solid var(--border);
  min-height: 0;
}
.side-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 10px 6px 14px;
}
.side-title { font-size: 11px; font-weight: 600; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.06em; }
.side-title em { font-style: normal; color: var(--text-dim); }
.side-actions { display: flex; gap: 2px; }
.table-filter { margin: 0 10px 8px; }
.table-list { flex: 1; overflow-y: auto; padding: 0 6px 10px; }
.empty { padding: 10px; text-align: center; }
.table-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 5px 8px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text);
  font-size: 12.5px;
  cursor: pointer;
  text-align: left;
}
.table-item:hover { background: var(--panel-3); }
.table-item.active { background: var(--accent-dim); }
.tbl-ic { width: 13px; height: 13px; color: var(--text-faint); flex: none; }
.table-item.active .tbl-ic { color: var(--accent); }
.tbl-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tbl-more {
  opacity: 0;
  color: var(--text-faint);
  padding: 0 2px;
  border-radius: 3px;
}
.table-item:hover .tbl-more, .table-item.active .tbl-more { opacity: 1; }
.tbl-more:hover { color: var(--text); background: var(--panel-3); }
.spin { display: inline-block; animation: rot 0.8s linear infinite; }
@keyframes rot { to { transform: rotate(360deg); } }
</style>
