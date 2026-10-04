<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { store, toast, loadConnections, activate, confirmBox, closeSession, sessionOf } from '../stores/app'
import { ConnectionService, errText } from '../lib/api'
import type { Connection, Hop } from '../lib/types'
import { clone, emptyHop, emptyNode } from '../lib/types'

const editing = computed(() => store.modal.editConnectionId !== '')
const isCluster = computed(() => form.nodes.length > 0)
const tab = ref<'basic' | 'net'>('basic')

const form = reactive<Connection>({
  id: '',
  name: '',
  scheme: 'http',
  host: '127.0.0.1',
  port: 9308,
  username: '',
  password: '',
  nodes: [],
  hops: [],
})

const view = ref<'list' | 'form'>('list')
const testing = ref(false)
const testResult = ref<{ ok: boolean; text: string } | null>(null)
const saving = ref(false)
const formError = ref('')

watch(
  () => store.modal.connection,
  (open) => {
    if (!open) return
    testResult.value = null
    formError.value = ''
    tab.value = 'basic'
    if (store.modal.editConnectionId) {
      const c = store.connections.find((x) => x.id === store.modal.editConnectionId)
      if (c) {
        fillForm(c)
        view.value = 'form'
        return
      }
    }
    view.value = 'list'
  },
)

function fillForm(c: Connection) {
  Object.assign(form, clone(c))
  if (!form.nodes) form.nodes = []
  if (!form.hops) form.hops = []
}

function newConnection() {
  fillForm({
    id: '',
    name: '',
    scheme: 'http',
    host: '127.0.0.1',
    port: 9308,
    username: '',
    password: '',
    nodes: [],
    hops: [],
  })
  view.value = 'form'
}

function editConnection(id: string) {
  store.modal.editConnectionId = id
  const c = store.connections.find((x) => x.id === id)
  if (c) {
    fillForm(c)
    view.value = 'form'
  }
  testResult.value = null
  formError.value = ''
}

function close() {
  store.modal.connection = false
}

// ------------------------------------------------------------- hop chain

function addHop(type: Hop['type']) {
  form.hops.push(emptyHop(type))
}

function removeHop(i: number) {
  form.hops.splice(i, 1)
}

function moveHop(i: number, dir: -1 | 1) {
  const j = i + dir
  if (j < 0 || j >= form.hops.length) return
  const [h] = form.hops.splice(i, 1)
  form.hops.splice(j, 0, h)
}

const activeHops = computed(() => form.hops.filter((h) => !h.disabled))
const hasSSH = computed(() => form.hops.some((h) => h.type === 'ssh' && !h.disabled))

function hopBadge(h: Hop) {
  return `${h.type}://${h.host}:${h.port || (h.type === 'ssh' ? 22 : h.type === 'http' ? 8080 : 1080)}`
}

async function clearHostKey(h: Hop) {
  try {
    await ConnectionService.ClearSSHHostKey(h.host, h.port || 22)
    toast('已清除该 SSH 服务器的主机密钥记录', 'success')
  } catch (e) {
    toast(errText(e), 'error')
  }
}

async function browseKey(h: Hop) {
  try {
    const path = await ConnectionService.PickKeyFile()
    if (path) h.keyPath = path
  } catch (e) {
    toast(errText(e), 'error')
  }
}

// ------------------------------------------------------------- cluster

function setCluster(on: boolean) {
  if (on && form.nodes.length === 0) {
    form.nodes = [{ scheme: form.scheme, host: form.host || '127.0.0.1', port: form.port || 9308 }]
  }
  if (!on) form.nodes = []
}

function addNode() {
  form.nodes.push(emptyNode())
}

function removeNode(i: number) {
  form.nodes.splice(i, 1)
}

// ------------------------------------------------------------- actions

async function test() {
  testResult.value = null
  testing.value = true
  try {
    const r = await ConnectionService.TestConnection(clone(form))
    testResult.value = {
      ok: true,
      text: `✓ 连接成功 — Manticore v${r?.version || '?'} · ${r?.via} · ${r?.tookMs.toFixed(0)} ms`,
    }
  } catch (e) {
    testResult.value = { ok: false, text: '✗ ' + errText(e) }
  } finally {
    testing.value = false
  }
}

async function save() {
  formError.value = ''
  saving.value = true
  try {
    const saved = await ConnectionService.SaveConnection(clone(form))
    const isNew = !store.connections.some((c) => c.id === saved.id)
    await loadConnections()
    if (isNew || store.sessions[saved.id]) {
      await activate(saved.id, { reconnect: true })
    }
    toast('连接已保存', 'success')
    close()
  } catch (e) {
    formError.value = errText(e)
  } finally {
    saving.value = false
  }
}

async function remove(c: Connection) {
  const ok = await confirmBox(`删除连接「${c.name}」?`, '删除连接')
  if (!ok) return
  try {
    if (store.sessions[c.id]) closeSession(c.id)
    await ConnectionService.DeleteConnection(c.id)
    await loadConnections()
    toast('连接已删除', 'success')
  } catch (e) {
    toast(errText(e), 'error')
  }
}

function sessionDot(id: string) {
  const s = sessionOf(id)
  if (!s) return 'gray'
  if (s.connecting) return 'yellow'
  return s.connected ? 'green' : 'red'
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
  <div v-if="store.modal.connection" class="modal-overlay" @click.self="close()">
    <div class="modal" :style="{ width: view === 'form' ? '720px' : '560px' }">
      <!-- ============ list view ============ -->
      <template v-if="view === 'list'">
        <div class="modal-head">
          <h3>连接管理</h3>
          <button class="modal-close" @click="close()">×</button>
        </div>
        <div class="modal-body" style="padding: 8px">
          <div v-if="store.connections.length === 0" class="hint" style="padding: 20px; text-align: center">
            还没有保存的连接
          </div>
          <div v-for="c in store.connections" :key="c.id" class="conn-row">
            <span class="dot" :class="sessionDot(c.id)"></span>
            <div class="conn-main">
              <div class="conn-name">{{ c.name }}</div>
              <div class="conn-sub mono">{{ connSubtitle(c) }}</div>
            </div>
            <span v-if="c.id === store.activeId" class="badge">当前</span>
            <span v-else-if="store.openIds.includes(c.id)" class="badge">已打开</span>
            <button class="btn sm" @click="activate(c.id); close()">{{ store.openIds.includes(c.id) ? '切换' : '打开' }}</button>
            <button class="btn ghost sm" @click="editConnection(c.id)">编辑</button>
            <button class="btn ghost sm danger" @click="remove(c)">删除</button>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn primary" @click="newConnection()">＋ 新建连接</button>
          <span class="flex1"></span>
          <button class="btn" @click="close()">关闭</button>
        </div>
      </template>

      <!-- ============ form view ============ -->
      <template v-else>
        <div class="modal-head">
          <h3>{{ editing ? '编辑连接' : '新建连接' }}</h3>
          <button class="modal-close" @click="editing && store.connections.length ? (view = 'list') : close()">×</button>
        </div>
        <div class="form-tabs">
          <button class="ftab" :class="{ on: tab === 'basic' }" @click="tab = 'basic'">基础配置</button>
          <button class="ftab" :class="{ on: tab === 'net' }" @click="tab = 'net'">
            网络链路拓扑 (代理链 / 隧道链)
            <span v-if="activeHops.length" class="ftab-badge">{{ activeHops.length }}</span>
          </button>
        </div>

        <div class="modal-body">
          <!-- ================= 基础配置 ================= -->
          <template v-if="tab === 'basic'">
            <div class="form-grid">
              <div>
                <label>名称</label>
                <input v-model="form.name" placeholder="如:本地开发" />
              </div>
              <div class="row2">
                <div>
                  <label>用户名(Manticore 鉴权,可选)</label>
                  <input v-model="form.username" autocomplete="off" />
                </div>
                <div>
                  <label>密码</label>
                  <input v-model="form.password" type="password" autocomplete="off" />
                </div>
              </div>
            </div>

            <div class="section-title">部署</div>
            <div class="mode-cards">
              <button class="mode-card" :class="{ on: !isCluster }" @click="setCluster(false)">
                <span class="mc-title">单机</span>
                <span class="mc-sub">连接单个 Manticore 实例</span>
              </button>
              <button class="mode-card" :class="{ on: isCluster }" @click="setCluster(true)">
                <span class="mc-title">集群</span>
                <span class="mc-sub">多节点轮询与故障转移</span>
              </button>
            </div>
            <div v-if="!isCluster" class="row3" style="margin-top: 10px">
              <div>
                <label>协议</label>
                <select v-model="form.scheme">
                  <option value="http">http</option>
                  <option value="https">https</option>
                </select>
              </div>
              <div>
                <label>主机</label>
                <input v-model="form.host" placeholder="127.0.0.1" />
              </div>
              <div>
                <label>端口</label>
                <input v-model.number="form.port" type="number" min="1" max="65535" />
              </div>
            </div>

            <!-- cluster nodes -->
            <div v-if="isCluster" class="net-config">
              <div v-for="(n, i) in form.nodes" :key="i" class="node-row">
                <span class="hop-no">{{ i + 1 }}</span>
                <select v-model="n.scheme" style="width: 96px">
                  <option value="http">http</option>
                  <option value="https">https</option>
                </select>
                <input v-model="n.host" placeholder="主机" style="flex: 1" />
                <input v-model.number="n.port" type="number" placeholder="端口" style="width: 84px" />
                <button class="btn ghost sm" title="移除节点" :disabled="form.nodes.length <= 1" @click="removeNode(i)">×</button>
              </div>
              <div style="margin-top: 8px">
                <button class="btn sm" @click="addNode">＋ 添加节点</button>
                <span class="hint" style="margin-left: 8px">
                  请求在节点间轮询分发,拨号失败的节点自动跳过(故障转移)。
                </span>
              </div>
            </div>
          </template>

          <!-- ================= 网络链路拓扑 ================= -->
          <template v-else>
            <!-- chain preview -->
            <div class="chain-preview">
              <div class="chain-head">
                <span>当前网络链路 (自左向右依次穿透)</span>
                <span class="hint">共 {{ activeHops.length }} 个有效穿透节点</span>
              </div>
              <div class="chain-flow">
                <span class="chain-node local">本机</span>
                <template v-if="activeHops.length">
                  <template v-for="(h, i) in activeHops" :key="i">
                    <span class="chain-arrow">→</span>
                    <span class="chain-node" :class="'t-' + h.type">{{ hopBadge(h) }}</span>
                  </template>
                </template>
                <span class="chain-arrow">→</span>
                <span v-if="!activeHops.length" class="chain-node direct">直接连通 (无代理)</span>
                <span v-else class="chain-node target">Manticore</span>
              </div>
            </div>

            <div class="list-head">
              <span class="section-title" style="margin: 0">代理 & 隧道节点列表</span>
              <span class="flex1"></span>
              <button class="btn sm" @click="addHop('http')">＋ HTTP 代理</button>
              <button class="btn sm" @click="addHop('socks5')">＋ SOCKS5 代理</button>
              <button class="btn sm" @click="addHop('ssh')">＋ SSH 隧道</button>
            </div>

            <div v-if="form.hops.length === 0" class="chain-empty">
              暂无配置代理/隧道节点,当前为直连模式。<br />
              如需多层穿透,可点击右上角按钮自由添加 HTTP、SOCKS5 代理或 SSH 跳板机并任意调整顺序。
            </div>

            <div v-for="(h, i) in form.hops" :key="i" class="hop-card" :class="{ off: h.disabled }">
              <div class="hop-line">
                <span class="hop-no">{{ i + 1 }}</span>
                <span class="badge" :class="'hop-' + h.type">{{ h.type === 'ssh' ? 'SSH 隧道' : h.type === 'http' ? 'HTTP 代理' : 'SOCKS5 代理' }}</span>
                <input v-model="h.host" placeholder="主机" style="flex: 1" />
                <input v-model.number="h.port" type="number" placeholder="端口" style="width: 84px" />
                <label class="hop-toggle" :title="h.disabled ? '已停用,点击启用' : '点击临时停用这一跳'">
                  <input type="checkbox" :checked="!h.disabled" @change="h.disabled = !($event.target as HTMLInputElement).checked" />
                  启用
                </label>
                <button class="btn ghost sm" title="上移" :disabled="i === 0" @click="moveHop(i, -1)">↑</button>
                <button class="btn ghost sm" title="下移" :disabled="i === form.hops.length - 1" @click="moveHop(i, 1)">↓</button>
                <button class="btn ghost sm danger" title="移除" @click="removeHop(i)">×</button>
              </div>
              <div v-if="h.type !== 'ssh'" class="hop-line sub">
                <input v-model="h.username" placeholder="用户名(可选)" style="width: 180px" />
                <input v-model="h.password" type="password" placeholder="密码(可选)" style="width: 180px" autocomplete="off" />
                <span class="hint">穿透目标:{{ h.remoteHost ? `${h.remoteHost}:${h.remotePort || 9308}` : '自动(下一跳 / Manticore)' }}</span>
              </div>
              <template v-else>
                <div class="hop-line sub">
                  <input v-model="h.username" placeholder="SSH 用户" style="width: 120px" />
                  <select v-model="h.authType" style="width: 96px">
                    <option value="password">密码</option>
                    <option value="key">私钥</option>
                  </select>
                  <input
                    v-if="h.authType === 'password'"
                    v-model="h.password"
                    type="password"
                    placeholder="SSH 密码"
                    style="flex: 1"
                    autocomplete="off"
                  />
                  <template v-else>
                    <input v-model="h.keyPath" placeholder="私钥路径 (~/.ssh/id_ed25519)" style="flex: 1" />
                    <button class="btn sm" @click="browseKey(h)">浏览…</button>
                    <input v-model="h.keyPassphrase" type="password" placeholder="私钥口令(可选)" style="width: 150px" autocomplete="off" />
                  </template>
                </div>
                <div class="hop-line sub">
                  <span class="hint" style="flex: 1">
                    隧道目标(SSH 服务器视角,留空自动): 
                    <input v-model="h.remoteHost" placeholder="主机" style="width: 150px; margin-left: 4px" />
                    <input v-model.number="h.remotePort" type="number" placeholder="端口" style="width: 84px; margin-left: 4px" />
                  </span>
                  <button class="btn ghost sm" @click="clearHostKey(h)">清除主机密钥记录</button>
                </div>
              </template>
            </div>

            <div v-if="hasSSH" class="hint" style="margin-top: 8px">
              SSH 主机密钥采用 TOFU 首次信任;若服务器密钥变更导致连接被拒,可清除对应节点的记录。
            </div>
          </template>

          <div v-if="testResult" :class="testResult.ok ? 'info-banner' : 'error-banner'" style="margin-top: 12px">
            {{ testResult.text }}
          </div>
          <div v-if="formError" class="error-banner" style="margin-top: 12px">{{ formError }}</div>
        </div>
        <div class="modal-foot">
          <button class="btn" :disabled="testing" @click="test">
            <span v-if="testing" class="spinner"></span>
            测试连通性
          </button>
          <span class="flex1"></span>
          <button v-if="editing" class="btn ghost" @click="view = 'list'">返回列表</button>
          <button class="btn" @click="close()">取消</button>
          <button class="btn primary" :disabled="saving" @click="save">
            <span v-if="saving" class="spinner"></span>
            保存配置
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.flex1 { flex: 1; }
.form-grid { display: flex; flex-direction: column; gap: 12px; }
.row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.row3 { display: grid; grid-template-columns: 110px 1fr 110px; gap: 10px; }

.form-tabs {
  display: flex;
  gap: 2px;
  padding: 0 16px;
  border-bottom: 1px solid var(--border-soft);
  flex: none;
}
.ftab {
  padding: 9px 14px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-dim);
  font-size: 12.5px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.ftab:hover { color: var(--text); }
.ftab.on { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
.ftab-badge {
  background: var(--accent);
  color: #fff;
  border-radius: 10px;
  font-size: 10px;
  padding: 0 6px;
  line-height: 15px;
}

.mode-cards { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.mode-card {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  background: var(--bg-deep);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  cursor: pointer;
  text-align: left;
}
.mode-card:hover { border-color: #323a52; }
.mode-card.on { border-color: var(--accent); background: var(--accent-dim); }
.mc-title { font-weight: 600; font-size: 12.5px; }
.mc-sub { font-size: 11px; color: var(--text-dim); }

.net-config {
  margin-top: 12px;
  padding: 12px;
  background: var(--bg-deep);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.node-row, .hop-line { display: flex; align-items: center; gap: 6px; }
.hop-line.sub { padding-left: 28px; margin-top: 4px; }
.hop-line.sub .hint { display: inline-flex; align-items: center; }

.chain-preview {
  background: var(--bg-deep);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  margin-bottom: 12px;
}
.chain-head {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-dim);
  margin-bottom: 8px;
}
.chain-flow {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.chain-node {
  padding: 3px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--panel-2);
  font-family: var(--mono);
  font-size: 11.5px;
}
.chain-node.local { font-weight: 600; }
.chain-node.direct { color: var(--green); border-color: rgba(62, 207, 142, 0.4); background: var(--green-dim); }
.chain-node.target { color: var(--accent); border-color: rgba(76, 141, 255, 0.4); background: var(--accent-dim); font-weight: 600; }
.chain-node.t-ssh { color: var(--yellow); border-color: rgba(245, 184, 92, 0.4); }
.chain-node.t-http, .chain-node.t-socks5 { color: var(--text); }
.chain-arrow { color: var(--text-faint); }
.chain-empty {
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  padding: 22px 16px;
  text-align: center;
  color: var(--text-dim);
  font-size: 12.5px;
  line-height: 1.8;
  margin-top: 10px;
}
.list-head { display: flex; align-items: center; gap: 6px; margin-top: 4px; }

.hop-card {
  background: var(--bg-deep);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  margin-top: 10px;
}
.hop-card.off { opacity: 0.55; }
.badge.hop-ssh { color: var(--yellow); border-color: rgba(245, 184, 92, 0.4); background: var(--yellow-dim); }
.badge.hop-http { color: var(--accent); border-color: rgba(76, 141, 255, 0.4); background: var(--accent-dim); }
.badge.hop-socks5 { color: var(--green); border-color: rgba(62, 207, 142, 0.4); background: var(--green-dim); }
.hop-no {
  width: 20px; height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--panel-3);
  color: var(--text-dim);
  font-size: 11px;
  flex: none;
}
.hop-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-dim);
  margin: 0;
  white-space: nowrap;
  cursor: pointer;
}

.conn-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
}
.conn-row:hover { background: var(--panel-2); }
.conn-main { flex: 1; min-width: 0; }
.conn-name { font-weight: 600; font-size: 13px; }
.conn-sub { font-size: 11px; color: var(--text-faint); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
