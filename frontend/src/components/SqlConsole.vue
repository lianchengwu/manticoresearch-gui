<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { store, toast, useSessionId } from '../stores/app'
import { QueryService, errText } from '../lib/api'
import type { QueryResult } from '../lib/types'
import DataGrid from './DataGrid.vue'

const connId = useSessionId()
const editor = ref<HTMLTextAreaElement | null>(null)
const sql = ref('')
const running = ref(false)
const result = ref<QueryResult | null>(null)
const historyOpen = ref(false)
const history = ref<string[]>([])

const histKey = computed(() => `msgui.hist.${connId.value}`)

const rowCount = computed(() => result.value?.rows?.length ?? 0)

function loadHistory() {
  try {
    history.value = JSON.parse(localStorage.getItem(histKey.value) ?? '[]')
  } catch {
    history.value = []
  }
}

function pushHistory(q: string) {
  history.value = [q, ...history.value.filter((x) => x !== q)].slice(0, 100)
  localStorage.setItem(histKey.value, JSON.stringify(history.value))
}

async function run() {
  const q = sql.value.trim()
  if (!q) return
  if (!connId.value) return toast('请先连接', 'error')
  running.value = true
  try {
    const res = await QueryService.ExecuteSQL(connId.value, q)
    result.value = res
    if (res?.error) {
      toast('查询出错', 'error')
    } else {
      pushHistory(q)
    }
  } catch (e) {
    result.value = { columns: [], rows: [], total: null, tookMs: 0, message: '', error: errText(e) }
    toast(errText(e), 'error')
  } finally {
    running.value = false
  }
}

function onKeyDown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault()
    run()
  } else if (e.key === 'Tab') {
    e.preventDefault()
    const el = editor.value
    if (!el) return
    const s = el.selectionStart
    el.setRangeText('  ', s, el.selectionEnd, 'end')
    sql.value = el.value
  }
}

function useHistoryItem(q: string) {
  sql.value = q
  historyOpen.value = false
  editor.value?.focus()
}

function clearHistory() {
  history.value = []
  localStorage.removeItem(histKey.value)
}

function onGlobalKey(e: KeyboardEvent) {
  if (e.key === 'F8' && !e.ctrlKey && !e.metaKey) {
    if (store.activeId !== connId.value) return
    e.preventDefault()
    run()
  }
}

onMounted(() => {
  loadHistory()
  window.addEventListener('keydown', onGlobalKey)
  editor.value?.focus()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKey))
</script>

<template>
  <div class="console">
    <div class="editor-pane">
      <div class="editor-toolbar">
        <button class="btn primary sm" :disabled="running" @click="run()">
          <span v-if="running" class="spinner"></span>
          <span v-else>▶</span>
          运行 <kbd>Ctrl+↵</kbd>
        </button>
        <button class="btn ghost sm" @click="historyOpen = !historyOpen">
          ⏱ 历史 <span class="hint">({{ history.length }})</span>
        </button>
        <button class="btn ghost sm" @click="sql = ''; result = null; editor?.focus()">清空</button>
        <span class="flex1"></span>
        <span class="hint">Manticore SQL · 单条语句</span>
      </div>

      <textarea
        ref="editor"
        v-model="sql"
        class="editor"
        spellcheck="false"
        placeholder="SELECT * FROM mytable LIMIT 10"
        @keydown="onKeyDown"
      ></textarea>
    </div>

    <div v-if="historyOpen" class="history">
      <div class="history-head">
        <span>查询历史</span>
        <button class="btn ghost sm" @click="clearHistory">清空历史</button>
      </div>
      <div class="history-list">
        <div v-if="history.length === 0" class="hint" style="padding: 10px">暂无历史</div>
        <button v-for="(h, i) in history" :key="i" class="history-item" :title="h" @click="useHistoryItem(h)">
          {{ h }}
        </button>
      </div>
    </div>

    <div class="result-pane">
      <div v-if="result" class="result-meta">
        <template v-if="!result.error">
          <span v-if="rowCount > 0" class="ok">{{ rowCount }} 行</span>
          <span v-if="result.message" class="dim">{{ result.message }}</span>
          <span class="flex1"></span>
          <span class="dim mono">{{ result.tookMs.toFixed(1) }} ms</span>
        </template>
        <template v-else>
          <span class="err">查询失败</span>
        </template>
      </div>
      <div v-if="result?.error" class="pad"><div class="error-banner">{{ result.error }}</div></div>
      <DataGrid
        v-else-if="result && rowCount > 0"
        :columns="result.columns"
        :rows="result.rows"
      />
      <div v-else-if="result" class="pad hint">
        {{ result.message || '执行成功,无返回行' }}
      </div>
      <div v-else class="placeholder hint">按 <kbd>Ctrl+Enter</kbd> 或 <kbd>F8</kbd> 运行查询</div>
    </div>
  </div>
</template>

<style scoped>
.console {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  position: relative;
}
.editor-pane {
  flex: none;
  height: 34%;
  min-height: 120px;
  display: flex;
  flex-direction: column;
  border-bottom: 1px solid var(--border);
  background: var(--bg-deep);
}
.editor-toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.flex1 { flex: 1; }
kbd {
  font-family: var(--mono);
  font-size: 10.5px;
  background: var(--panel-3);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 5px;
  color: var(--text-dim);
}
.editor {
  flex: 1;
  resize: none;
  border: none;
  border-radius: 0;
  background: transparent;
  font-family: var(--mono);
  font-size: 13px;
  line-height: 1.55;
  padding: 12px 14px;
  color: var(--text);
}
.editor:focus { box-shadow: none; }

.history {
  position: absolute;
  top: 42px;
  left: 10px;
  width: 460px;
  max-height: 300px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-pop);
  z-index: 30;
  display: flex;
  flex-direction: column;
}
.history-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  font-size: 12px;
  color: var(--text-dim);
}
.history-list { overflow-y: auto; padding: 4px; }
.history-item {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  color: var(--text);
  font-family: var(--mono);
  font-size: 11.5px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.history-item:hover { background: var(--panel-3); }

.result-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.result-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--border-soft);
  font-size: 12px;
  flex: none;
}
.result-meta .ok { color: var(--green); font-weight: 600; }
.result-meta .err { color: var(--red); font-weight: 600; }
.dim { color: var(--text-dim); }
.pad { padding: 10px 12px; }
.placeholder {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
}
</style>
