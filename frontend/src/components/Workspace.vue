<script setup lang="ts">
import { computed, toRef } from 'vue'
import {
  closeTab,
  focusSession,
  provideSessionId,
  sessionOf,
} from '../stores/app'
import Sidebar from './Sidebar.vue'
import SqlConsole from './SqlConsole.vue'
import TableData from './TableData.vue'
import TableSchema from './TableSchema.vue'

const props = defineProps<{ connId: string }>()
provideSessionId(toRef(props, 'connId'))

const sess = computed(() => sessionOf(props.connId))
const activeTab = computed(() => sess.value?.tabs.find((t) => t.id === sess.value?.activeTabId))

function selectTab(id: string) {
  const s = sess.value
  if (!s) return
  s.activeTabId = id
  focusSession(props.connId)
}
</script>

<template>
  <div class="workspace">
    <Sidebar />
    <main class="main">
      <div class="tabbar">
        <button
          v-for="t in sess?.tabs ?? []"
          :key="t.id"
          class="tab"
          :class="{ active: t.id === sess?.activeTabId }"
          @click="selectTab(t.id)"
        >
          <svg v-if="t.kind === 'sql'" class="tab-ic" viewBox="0 0 16 16">
            <path d="M3 4l4 4-4 4M8.5 12H13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
          </svg>
          <svg v-else class="tab-ic" viewBox="0 0 16 16">
            <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.1" />
            <path d="M1.5 6h13M6 6v7.5" stroke="currentColor" stroke-width="1.1" fill="none" />
          </svg>
          <span>{{ t.kind === 'sql' ? 'SQL 控制台' : t.table }}</span>
          <span
            v-if="t.kind === 'table'"
            class="tab-close"
            title="关闭"
            @click.stop="closeTab(t.id, connId)"
          >×</span>
        </button>
      </div>
      <section class="tab-content">
        <SqlConsole v-if="activeTab?.kind === 'sql'" />
        <template v-else-if="activeTab?.kind === 'table'">
          <div class="view-toggle">
            <button
              class="vt"
              :class="{ on: activeTab.view === 'data' }"
              @click="activeTab.view = 'data'"
            >数据</button>
            <button
              class="vt"
              :class="{ on: activeTab.view === 'schema' }"
              @click="activeTab.view = 'schema'"
            >结构</button>
          </div>
          <TableData v-if="activeTab.view === 'data'" :table="activeTab.table!" />
          <TableSchema v-else :table="activeTab.table!" />
        </template>
      </section>
    </main>
  </div>
</template>

<style scoped>
.workspace {
  flex: 1;
  display: flex;
  min-width: 0;
  min-height: 0;
  background: var(--bg);
}
.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.tabbar {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  padding: 6px 8px 0;
  background: var(--bg-deep);
  border-bottom: 1px solid var(--border);
  flex: none;
  overflow-x: auto;
}
.tab {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 10px 6px 12px;
  background: none;
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  color: var(--text-dim);
  font-size: 12.5px;
  cursor: pointer;
  white-space: nowrap;
}
.tab:hover { color: var(--text); background: var(--panel); }
.tab.active {
  color: var(--text);
  background: var(--bg);
  border-color: var(--border);
  position: relative;
}
.tab.active::after {
  content: '';
  position: absolute;
  left: 0; right: 0; bottom: -1px;
  height: 1px;
  background: var(--bg);
}
.tab-ic { width: 13px; height: 13px; flex: none; }
.tab-close {
  margin-left: 2px;
  width: 16px; height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  color: var(--text-faint);
  font-size: 13px;
  line-height: 1;
}
.tab-close:hover { background: var(--red-dim); color: var(--red); }
.tab-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.view-toggle {
  display: flex;
  gap: 0;
  padding: 8px 12px 0;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.vt {
  padding: 5px 14px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-dim);
  font-size: 12.5px;
  cursor: pointer;
}
.vt:hover { color: var(--text); }
.vt.on { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
</style>
