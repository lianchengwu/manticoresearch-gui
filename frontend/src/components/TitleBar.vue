<script setup lang="ts">
import { Window } from '@wailsio/runtime'
import { computed, ref } from 'vue'
import {
  store,
  activate,
  closeSession,
  focusSession,
  openConnectionModal,
  sessionOf,
} from '../stores/app'
import type { Connection } from '../lib/types'

const pickerOpen = ref(false)
const theme = ref(document.documentElement.dataset.theme ?? 'dark')

function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = theme.value
  localStorage.setItem('msgui.theme', theme.value)
}

const openConns = computed(() =>
  store.openIds.map((id) => store.connections.find((c) => c.id === id)).filter(Boolean) as Connection[],
)

function sessDot(id: string) {
  const s = sessionOf(id)
  if (!s) return 'gray'
  if (s.connecting) return 'yellow'
  return s.connected ? 'green' : 'red'
}

function pickerDot(c: Connection) {
  const s = sessionOf(c.id)
  if (!s) return 'gray'
  if (s.connecting) return 'yellow'
  return s.connected ? 'green' : 'gray'
}

async function pick(c: Connection) {
  pickerOpen.value = false
  await activate(c.id)
}

function connSubtitle(c: Connection) {
  const nodes = c.nodes?.length ?? 0
  const base = nodes > 1 ? `集群 · ${nodes} 节点` : `${c.scheme}://${c.host}:${c.port}`
  const hopCount = (c.hops ?? []).filter((h) => !h.disabled).length
  if (hopCount > 0) return `${base} · ${hopCount} 跳穿透`
  return base
}
</script>

<template>
  <header class="titlebar" style="--wails-draggable: drag">
    <div class="brand">
      <img src="/logo.svg" alt="" class="logo" />
      <span class="app-name">ManticoreSearch <b>GUI</b></span>
    </div>

    <div class="conn-area" style="--wails-draggable: no-drag">
      <div class="conn-tabs">
        <button
          v-for="c in openConns"
          :key="c.id"
          class="conn-tab"
          :class="{ on: c.id === store.activeId }"
          @click="focusSession(c.id)"
        >
          <span class="dot" :class="sessDot(c.id)"></span>
          <span class="conn-name">{{ c.name }}</span>
          <span class="tab-x" title="关闭标签" @click.stop="closeSession(c.id)">×</span>
        </button>
        <button v-if="openConns.length === 0" class="conn-tab" @click="pickerOpen = !pickerOpen">
          <span class="dot gray"></span>
          <span class="conn-name">未连接</span>
        </button>
        <button class="conn-add" title="打开连接" @click="pickerOpen = !pickerOpen">＋</button>
      </div>

      <div v-if="pickerOpen" class="picker-overlay" @click="pickerOpen = false"></div>
      <div v-if="pickerOpen" class="picker">
        <div v-if="store.connections.length === 0" class="picker-empty hint">还没有连接</div>
        <button
          v-for="c in store.connections"
          :key="c.id"
          class="picker-item"
          :class="{ active: c.id === store.activeId }"
          @click="pick(c)"
        >
          <span class="pi-name">
            <span class="dot" :class="pickerDot(c)"></span>
            {{ c.name }}
            <span v-if="store.openIds.includes(c.id)" class="open-tag">已打开</span>
          </span>
          <span class="pi-sub">{{ connSubtitle(c) }}</span>
        </button>
        <div class="picker-sep"></div>
        <button class="picker-item" @click="pickerOpen = false; openConnectionModal('')">
          <span class="pi-name">＋ 新建连接…</span>
        </button>
        <button
          v-if="store.activeId"
          class="picker-item"
          @click="pickerOpen = false; openConnectionModal(store.activeId)"
        >
          <span class="pi-name">⚙ 编辑当前连接…</span>
        </button>
      </div>
    </div>

    <div class="spacer"></div>

    <button class="theme-btn" style="--wails-draggable: no-drag" :title="theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'" @click="toggleTheme">
      <svg v-if="theme === 'dark'" viewBox="0 0 16 16" class="theme-ic">
        <circle cx="8" cy="8" r="3.4" fill="none" stroke="currentColor" stroke-width="1.2" />
        <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6L13 13M13 3l-1.4 1.4M4.4 11.6L3 13" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
      </svg>
      <svg v-else viewBox="0 0 16 16" class="theme-ic">
        <path d="M13.5 9.5A6 6 0 1 1 6.5 2.5a5 5 0 0 0 7 7z" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
      </svg>
    </button>

    <div class="win-controls" style="--wails-draggable: no-drag">
      <button class="wc" title="最小化" @click="Window.Minimise()">
        <svg viewBox="0 0 12 12"><path d="M1 6h10" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
      <button class="wc" title="最大化/还原" @click="Window.ToggleMaximise()">
        <svg viewBox="0 0 12 12"><rect x="1.5" y="1.5" width="9" height="9" rx="1" fill="none" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
      <button class="wc close" title="关闭" @click="Window.Close()">
        <svg viewBox="0 0 12 12"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  height: var(--titlebar-h);
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 0 0 12px;
  background: var(--titlebar-grad);
  border-bottom: 1px solid var(--border);
}
.brand { display: flex; align-items: center; gap: 8px; }
.logo { width: 18px; height: 18px; border-radius: 4px; }
.app-name { font-size: 12.5px; color: var(--text-dim); letter-spacing: 0.02em; }
.app-name b { color: var(--text); font-weight: 600; }

.conn-area { position: relative; margin-left: 10px; display: flex; align-items: stretch; min-width: 0; }
.conn-tabs {
  display: flex;
  align-items: stretch;
  gap: 0;
  min-width: 0;
  max-width: 560px;
  overflow-x: auto;
}
.conn-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  height: var(--titlebar-h);
  border: none;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: none;
  color: var(--text-dim);
  font-size: 12.5px;
  cursor: pointer;
  flex: none;
}
.conn-tab:hover { color: var(--text); background: var(--panel-3); }
.conn-tab.on {
  color: var(--text);
  border-bottom-color: var(--accent);
  background: var(--accent-dim);
}
.conn-name { max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tab-x {
  width: 16px; height: 16px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 4px;
  color: var(--text-faint);
  font-size: 13px;
  line-height: 1;
}
.tab-x:hover { background: var(--red-dim); color: var(--red); }
.conn-add {
  width: 28px;
  height: var(--titlebar-h);
  border: none;
  background: none;
  color: var(--text-dim);
  cursor: pointer;
  font-size: 16px;
  flex: none;
}
.conn-add:hover { color: var(--text); background: var(--panel-3); }

.picker-overlay { position: fixed; inset: 0; z-index: 40; }
.picker {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  width: 300px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-pop);
  padding: 6px;
  z-index: 41;
}
.picker-empty { padding: 8px 10px; }
.picker-item {
  display: flex;
  flex-direction: column;
  gap: 1px;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  color: var(--text);
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 12.5px;
}
.picker-item:hover { background: var(--panel-3); }
.picker-item.active { background: var(--accent-dim); }
.pi-name { display: flex; align-items: center; gap: 8px; font-weight: 500; }
.pi-sub { font-size: 11px; color: var(--text-faint); padding-left: 16px; font-family: var(--mono); }
.picker-sep { height: 1px; background: var(--border-soft); margin: 5px 4px; }
.open-tag {
  margin-left: auto;
  font-size: 10px;
  font-weight: 600;
  color: var(--green);
}

.spacer { flex: 1; }

.theme-btn {
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--text-dim);
  cursor: pointer;
  margin-right: 8px;
}
.theme-btn:hover { background: var(--panel-3); color: var(--text); }
.theme-ic { width: 15px; height: 15px; }

.win-controls { display: flex; height: 100%; }
.wc {
  width: 44px;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: var(--text-dim);
  cursor: pointer;
}
.wc:hover { background: var(--panel-3); color: var(--text); }
.wc.close:hover { background: #e5484d; color: #fff; }
.wc svg { width: 12px; height: 12px; }
</style>
