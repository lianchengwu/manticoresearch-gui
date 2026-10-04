<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { store, loadConnections, openConnectionModal, connById, sessionOf } from './stores/app'
import TitleBar from './components/TitleBar.vue'
import Workspace from './components/Workspace.vue'
import ConnectionModal from './components/ConnectionModal.vue'
import DocModal from './components/DocModal.vue'
import Toasts from './components/Toasts.vue'
import ConfirmModal from './components/ConfirmModal.vue'

const focused = computed(() => sessionOf(store.activeId))
const focusedConn = computed(() => connById(store.activeId))

onMounted(loadConnections)
</script>

<template>
  <TitleBar />

  <div class="body">
    <div v-if="store.openIds.length === 0" class="welcome">
      <img src="/logo.svg" class="welcome-logo" alt="" />
      <h1>ManticoreSearch GUI</h1>
      <p class="hint">连接到 Manticore Search 服务器,浏览表、执行 SQL、管理文档。<br />
        支持 SSH 隧道与 HTTP / SOCKS5 代理链。可同时打开多条连接,用顶栏标签切换。</p>
      <button v-if="store.connections.length === 0" class="btn primary" @click="openConnectionModal('')">
        ＋ 新建连接
      </button>
      <p v-else class="hint">通过顶栏打开连接；本地和远程可以各开一个标签</p>
    </div>
    <Workspace
      v-for="id in store.openIds"
      v-show="id === store.activeId"
      :key="id"
      :conn-id="id"
    />
  </div>

  <footer class="statusbar">
    <template v-if="focusedConn && focused">
      <span class="dot" :class="focused.connecting ? 'yellow' : focused.connected ? 'green' : 'red'"></span>
      <span>{{ focusedConn.name }}</span>
      <span class="sep">·</span>
      <span v-if="(focusedConn.nodes?.length ?? 0) > 1" class="mono">集群 · {{ focusedConn.nodes.length }} 节点</span>
      <span v-else class="mono">{{ focusedConn.scheme }}://{{ focusedConn.host }}:{{ focusedConn.port }}</span>
      <template v-if="focused.via && focused.via !== '直连'">
        <span class="sep">·</span><span>{{ focused.via }}</span>
      </template>
      <span class="flex1"></span>
      <span v-if="store.openIds.length > 1" class="hint">{{ store.openIds.length }} 个标签</span>
      <span v-if="focused.version" class="mono">v{{ focused.version }}</span>
      <span class="sep">·</span>
      <span>{{ focused.tables.length }} 张表</span>
    </template>
    <template v-else>
      <span class="hint">未连接</span>
    </template>
  </footer>

  <ConnectionModal />
  <DocModal />
  <ConfirmModal />
  <Toasts />
</template>

<style scoped>
.body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.welcome {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  text-align: center;
}
.welcome-logo { width: 56px; height: 56px; border-radius: 14px; }
.welcome h1 { margin: 0; font-size: 20px; font-weight: 700; }

.statusbar {
  height: var(--statusbar-h);
  flex: none;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 12px;
  background: var(--panel);
  border-top: 1px solid var(--border);
  font-size: 11.5px;
  color: var(--text-dim);
}
.statusbar .sep { color: var(--text-faint); }
.statusbar .mono, .mono { font-family: var(--mono); }
.flex1 { flex: 1; }
</style>
