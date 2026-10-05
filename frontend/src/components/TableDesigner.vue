<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import ColumnFields from './ColumnFields.vue'
import { closeDesigner, designer } from './tabledesigner'
import { TableService, errText } from '../lib/api'
import {
  FT_OPTIONS,
  canWiden,
  columnLabel,
  emptyColumn,
  emptyColumnDef,
  filledOptions,
  parseDescribe,
  toColumnDef,
  type ColumnDraft,
  type ExistingField,
  type PendingChange,
} from '../lib/schema'
import type { CreateTableSpec, OptionPair } from '../lib/types'
import { execStatements, ops } from '../lib/tableops'
import { bumpTable, openTableTab, refreshTables, retargetTableTab, toast } from '../stores/app'

const name = ref('')
const ifNotExists = ref(false)
const kind = ref<'rt' | 'pq' | 'distributed'>('rt')
const columns = ref<ColumnDraft[]>([])
const members = ref<Array<{ kind: 'local' | 'agent'; value: string }>>([])
const optionValues = reactive<Record<string, string>>({})
const customOptions = ref<OptionPair[]>([])
const engine = ref('')
const profile = ref('')
const pending = ref<PendingChange[]>([])
const sqlText = ref('')
const editSQL = ref(false)
const previewError = ref('')
const saving = ref(false)
const loading = ref(false)
const existing = ref<ExistingField[]>([])

const addCol = ref(emptyColumn())
const renameTo = ref('')
const tab = ref<'fields' | 'options'>('fields')
const nameInput = ref<HTMLInputElement | null>(null)
let seq = 0
let pendingSeq = 1
let timer = 0

function resetOptions() {
  for (const f of FT_OPTIONS) optionValues[f.name] = ''
  customOptions.value = []
  engine.value = ''
  profile.value = ''
}

function initCreate() {
  name.value = ''
  ifNotExists.value = false
  kind.value = 'rt'
  columns.value = [emptyColumn('title')]
  members.value = [{ kind: 'local', value: '' }]
  resetOptions()
  editSQL.value = false
  sqlText.value = ''
  previewError.value = ''
  tab.value = 'fields'
  existing.value = []
  pending.value = []
  addCol.value = emptyColumn()
  renameTo.value = ''
}

async function loadExisting() {
  loading.value = true
  try {
    const described = await TableService.DescribeTable(designer.connId, designer.table)
    if (described?.error) toast(described.error, 'error')
    existing.value = parseDescribe(described)
  } catch (e) {
    toast(errText(e), 'error')
    existing.value = []
  } finally {
    loading.value = false
  }
}

async function initDesign() {
  name.value = designer.table
  pending.value = []
  editSQL.value = false
  addCol.value = emptyColumn()
  renameTo.value = ''
  resetOptions()
  sqlText.value = ''
  previewError.value = ''
  await loadExisting()
}

watch(
  () => designer.open && designer.nonce,
  (token) => {
    if (!token) return
    if (designer.mode === 'create') initCreate()
    else void initDesign()
  },
)

function buildSpec(): CreateTableSpec {
  const options = filledOptions(optionValues, customOptions.value)
  if (profile.value) options.unshift({ name: 'profile', value: profile.value })
  return {
    name: name.value.trim(),
    ifNotExists: ifNotExists.value,
    kind: kind.value,
    columns: columns.value.map(toColumnDef),
    options,
    members: members.value.map((m) => ({ kind: m.kind, value: m.value.trim() })),
    engine: engine.value,
  }
}

const ordered = computed(() => {
  const rest = pending.value.filter((p) => p.change.action !== 'rename')
  const renames = pending.value.filter((p) => p.change.action === 'rename')
  return [...rest, ...renames]
})

async function refreshPreview() {
  if (!designer.open || editSQL.value) return
  const n = ++seq
  try {
    if (designer.mode === 'create') {
      const prev = await TableService.RenderCreateTable(buildSpec())
      if (n !== seq || editSQL.value) return
      previewError.value = prev?.error ?? ''
      sqlText.value = prev?.error ? '' : (prev?.sql ?? '')
    } else if (ordered.value.length === 0) {
      if (n !== seq || editSQL.value) return
      previewError.value = ''
      sqlText.value = ''
    } else {
      const prev = await TableService.RenderSchemaChanges(
        designer.table,
        ordered.value.map((p) => p.change),
      )
      if (n !== seq || editSQL.value) return
      previewError.value = prev?.error ?? ''
      sqlText.value = prev?.error ? '' : (prev?.sql ?? '')
    }
  } catch (e) {
    if (n !== seq || editSQL.value) return
    previewError.value = errText(e)
    sqlText.value = ''
  }
}

function schedulePreview() {
  window.clearTimeout(timer)
  timer = window.setTimeout(refreshPreview, 80)
}

watch([name, ifNotExists, kind, columns, members, customOptions, engine, profile, editSQL, pending], schedulePreview, { deep: true })
watch(optionValues, schedulePreview, { deep: true })

function move(i: number, dir: number) {
  const j = i + dir
  if (j < 0 || j >= columns.value.length) return
  const [row] = columns.value.splice(i, 1)
  if (row) columns.value.splice(j, 0, row)
}

function addPending(label: string, change: (typeof pending.value)[number]['change']) {
  if (change.action === 'rename') pending.value = pending.value.filter((p) => p.change.action !== 'rename')
  pending.value.push({ id: pendingSeq++, label, change })
}

function queueAdd() {
  if (!addCol.value.name.trim()) {
    toast('填写字段名', 'error')
    return
  }
  addPending('新增 ' + columnLabel(addCol.value), {
    action: 'add',
    column: toColumnDef(addCol.value),
    newName: '',
    settings: [],
  })
  addCol.value = emptyColumn()
}

function queued(action: string, field: string) {
  return pending.value.some((p) => p.change.action === action && p.change.column.name === field)
}

function queueDrop(field: ExistingField) {
  if (field.name.toLowerCase() === 'id' || queued('drop', field.name)) return
  addPending('删除 ' + field.name, {
    action: 'drop',
    column: { ...emptyColumnDef(), name: field.name },
    newName: '',
    settings: [],
  })
}

function queueWiden(field: ExistingField) {
  if (queued('modify', field.name)) return
  addPending(field.name + ' → bigint', {
    action: 'modify',
    column: { ...emptyColumnDef(), name: field.name },
    newName: '',
    settings: [],
  })
}

function queueRename() {
  const next = renameTo.value.trim()
  if (!next) return
  addPending('重命名为 ' + next, {
    action: 'rename',
    column: emptyColumnDef(),
    newName: next,
    settings: [],
  })
  renameTo.value = ''
}

function queueSettings() {
  const settings = filledOptions(optionValues, customOptions.value)
  if (settings.length === 0) {
    toast('先填写要修改的设置', 'error')
    return
  }
  addPending('设置 ' + settings.map((s) => s.name).join(', '), {
    action: 'setting',
    column: emptyColumnDef(),
    newName: '',
    settings,
  })
  resetOptions()
}

const canSubmit = computed(() => {
  if (saving.value) return false
  if (editSQL.value) return sqlText.value.trim().length > 0
  if (previewError.value) return false
  if (designer.mode === 'create') return name.value.trim().length > 0 && sqlText.value.trim().length > 0
  return ordered.value.length > 0 && sqlText.value.trim().length > 0
})

async function submit() {
  if (!canSubmit.value) return
  saving.value = true
  const connId = designer.connId
  try {
    if (editSQL.value) {
      const ok = await execStatements(connId, sqlText.value)
      if (!ok) {
        if (designer.mode === 'design') await loadExisting()
        return
      }
      toast('已执行', 'success')
      await refreshTables(connId)
      if (designer.mode === 'design') {
        bumpTable(designer.table)
        await initDesign()
      } else {
        closeDesigner()
      }
      return
    }
    if (designer.mode === 'create') {
      const spec = buildSpec()
      const res = await TableService.CreateTable(connId, spec)
      if (res?.error) {
        toast(res.error, 'error')
        return
      }
      toast('表已创建', 'success')
      closeDesigner()
      await refreshTables(connId)
      openTableTab(spec.name, connId, 'schema')
      return
    }
    const batch = ordered.value
    const res = await TableService.ApplySchema(connId, designer.table, batch.map((p) => p.change))
    if (!res) {
      toast('没有返回结果', 'error')
      return
    }
    if (res.applied > 0) {
      const ran = new Set(batch.slice(0, res.applied).map((p) => p.id))
      const renamed = batch.slice(0, res.applied).find((p) => p.change.action === 'rename')
      pending.value = pending.value.filter((p) => !ran.has(p.id))
      if (renamed?.change.newName) {
        retargetTableTab(designer.table, renamed.change.newName, connId)
        designer.table = renamed.change.newName
      }
      await refreshTables(connId)
      bumpTable(designer.table)
      await loadExisting()
    }
    if (res.error) {
      toast(res.failedSql ? `${res.error}\n${res.failedSql}` : res.error, 'error')
      return
    }
    toast(res.message || '结构已更新', 'success')
  } catch (e) {
    toast(errText(e), 'error')
  } finally {
    saving.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key !== 'Escape' || !designer.open || ops.promptOpen) return
  closeDesigner()
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.clearTimeout(timer)
})
</script>

<template>
  <div v-if="designer.open" class="modal-overlay" @click.self="closeDesigner()">
    <div class="modal designer">
      <div class="modal-head">
        <h3>{{ designer.mode === 'create' ? '新建表' : '设计表 ' + designer.table }}</h3>
        <button class="modal-close" @click="closeDesigner()">×</button>
      </div>

      <div class="modal-body">
        <template v-if="designer.mode === 'create'">
          <div class="mode-cards">
            <button class="mode-card" :class="{ on: kind === 'rt' }" type="button" @click="kind = 'rt'">
              <span class="mc-title">实时表</span>
              <span class="mc-sub">rt · 可写入、可改结构</span>
            </button>
            <button class="mode-card" :class="{ on: kind === 'pq' }" type="button" @click="kind = 'pq'">
              <span class="mc-title">渗滤表</span>
              <span class="mc-sub">type='pq' · 存查询规则</span>
            </button>
            <button class="mode-card" :class="{ on: kind === 'distributed' }" type="button" @click="kind = 'distributed'">
              <span class="mc-title">分布式</span>
              <span class="mc-sub">聚合本地表和远程 agent</span>
            </button>
          </div>
          <div class="name-row">
            <div>
              <label>表名</label>
              <input ref="nameInput" v-model="name" placeholder="products" spellcheck="false" />
            </div>
            <label class="chk ifne"><input v-model="ifNotExists" type="checkbox" />IF NOT EXISTS</label>
          </div>
          <p v-if="kind === 'pq'" class="hint">字段是待匹配文档的结构。query / filters / tags 由 Manticore 自动提供。</p>
          <p v-else-if="kind === 'rt'" class="hint">id 由 Manticore 自动创建，不必声明。</p>
        </template>

        <div v-if="designer.mode === 'design'" class="design-head">
          <span class="badge type-rt">{{ designer.table }}</span>
          <span class="hint">已有字段只能新增、删除，或把 int 扩成 bigint。全文设置只影响之后写入的文档。</span>
        </div>

        <div v-if="designer.mode === 'create' && kind !== 'distributed'" class="form-tabs">
          <button class="ftab" :class="{ on: tab === 'fields' }" type="button" @click="tab = 'fields'">字段</button>
          <button class="ftab" :class="{ on: tab === 'options' }" type="button" @click="tab = 'options'">选项</button>
        </div>

        <div v-if="loading" class="loading-row"><span class="spinner"></span></div>

        <template v-else-if="designer.mode === 'design'">
          <div class="section-title">现有字段</div>
          <table class="grid fields">
            <thead>
              <tr><th>字段</th><th>类型</th><th>属性</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="f in existing" :key="f.name + f.type">
                <td class="mono">{{ f.name }}</td>
                <td>{{ f.type }}</td>
                <td class="dim">{{ f.properties || '—' }}</td>
                <td class="acts">
                  <button
                    v-if="canWiden(f.type)"
                    class="btn ghost sm"
                    type="button"
                    :disabled="queued('modify', f.name)"
                    @click="queueWiden(f)"
                  >扩为 bigint</button>
                  <button
                    class="btn ghost sm danger"
                    type="button"
                    :disabled="f.name.toLowerCase() === 'id' || queued('drop', f.name)"
                    @click="queueDrop(f)"
                  >删除</button>
                </td>
              </tr>
            </tbody>
          </table>

          <div class="section-title">新增字段</div>
          <ColumnFields :col="addCol" />
          <button class="btn sm" type="button" @click="queueAdd">加入变更</button>

          <div class="section-title">重命名</div>
          <div class="inline">
            <input v-model="renameTo" placeholder="新表名" spellcheck="false" />
            <button class="btn sm" type="button" @click="queueRename">加入变更</button>
          </div>

          <div class="section-title">全文设置</div>
          <p class="hint">只作用于新文档。已有文档需重新写入后才会用新分词。</p>
          <div class="opt-grid">
            <div v-for="f in FT_OPTIONS" :key="f.name">
              <label>{{ f.label }}</label>
              <input v-model="optionValues[f.name]" :placeholder="f.placeholder" spellcheck="false" />
            </div>
          </div>
          <button class="btn sm" type="button" style="margin-top: 8px" @click="queueSettings">把已填设置加入变更</button>

          <div class="section-title">待应用</div>
          <div v-if="ordered.length === 0" class="hint">还没有变更</div>
          <div v-for="p in ordered" :key="p.id" class="pending">
            <span>{{ p.label }}</span>
            <button class="btn ghost sm" type="button" @click="pending = pending.filter((x) => x.id !== p.id)">×</button>
          </div>
        </template>

        <template v-else-if="kind === 'distributed'">
          <div class="section-title">成员</div>
          <p class="hint">local 是本机表名，多个用逗号分隔。agent 格式 host:9312:table，镜像用 | 分隔。</p>
          <div v-for="(m, i) in members" :key="i" class="inline">
            <select v-model="m.kind">
              <option value="local">local</option>
              <option value="agent">agent</option>
            </select>
            <input
              v-model="m.value"
              :placeholder="m.kind === 'local' ? '本地表名' : '127.0.0.1:9312:products'"
              spellcheck="false"
            />
            <button class="btn ghost sm" type="button" @click="members.splice(i, 1)">×</button>
          </div>
          <button class="btn sm" type="button" @click="members.push({ kind: 'local', value: '' })">＋ 成员</button>
        </template>

        <template v-else-if="tab === 'fields'">
          <div class="col-list">
            <div v-for="(c, i) in columns" :key="i" class="col-line">
              <ColumnFields :col="c" />
              <div class="col-acts">
                <button class="btn ghost sm" type="button" :disabled="i === 0" @click="move(i, -1)">↑</button>
                <button class="btn ghost sm" type="button" :disabled="i === columns.length - 1" @click="move(i, 1)">↓</button>
                <button class="btn ghost sm danger" type="button" @click="columns.splice(i, 1)">×</button>
              </div>
            </div>
          </div>
          <button class="btn sm" type="button" @click="columns.push(emptyColumn())">＋ 字段</button>
        </template>

        <template v-else>
          <div class="opt-grid">
            <div>
              <label>profile</label>
              <select v-model="profile">
                <option value="">不使用</option>
                <option value="relevance">relevance</option>
              </select>
            </div>
            <div>
              <label>engine</label>
              <select v-model="engine">
                <option value="">默认</option>
                <option value="columnar">columnar</option>
              </select>
            </div>
            <div v-for="f in FT_OPTIONS" :key="f.name">
              <label>{{ f.label }}</label>
              <input v-model="optionValues[f.name]" :placeholder="f.placeholder" spellcheck="false" />
            </div>
          </div>
          <div class="section-title">自定义选项</div>
          <div v-for="(o, i) in customOptions" :key="i" class="inline">
            <input v-model="o.name" placeholder="选项名" spellcheck="false" />
            <input v-model="o.value" placeholder="值" spellcheck="false" />
            <button class="btn ghost sm" type="button" @click="customOptions.splice(i, 1)">×</button>
          </div>
          <button class="btn sm" type="button" @click="customOptions.push({ name: '', value: '' })">＋ 选项</button>
        </template>
      </div>

      <div class="sql-dock">
        <div class="sql-dock-head">
          <span>SQL</span>
          <label class="chk"><input v-model="editSQL" type="checkbox" />直接编辑</label>
        </div>
        <div v-if="previewError && !editSQL" class="error-banner">{{ previewError }}</div>
        <textarea v-else-if="editSQL" v-model="sqlText" class="sql-edit" rows="5" spellcheck="false"></textarea>
        <pre v-else class="sql-pre">{{ sqlText || '—' }}</pre>
      </div>
      <div class="modal-foot">
        <span class="hint">{{ designer.mode === 'design' ? 'ALTER 逐条执行，失败时停在出错的那条' : '执行前可改 SQL' }}</span>
        <span class="flex1"></span>
        <button class="btn" type="button" @click="closeDesigner()">取消</button>
        <button class="btn primary" type="button" :disabled="!canSubmit" @click="submit">
          {{ saving ? '执行中…' : designer.mode === 'create' ? '创建' : '应用变更' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.designer { width: min(880px, calc(100vw - 32px)); }
.flex1 { flex: 1; }
.mode-cards { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 8px; }
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
.name-row { display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: end; margin-top: 12px; }
.chk { display: inline-flex; align-items: center; gap: 6px; margin: 0; cursor: pointer; }
.ifne { margin-bottom: 8px; }
.form-tabs { display: flex; gap: 2px; margin: 12px -16px 0; padding: 0 16px; border-bottom: 1px solid var(--border-soft); }
.ftab {
  padding: 8px 12px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-dim);
  font-size: 12.5px;
  cursor: pointer;
}
.ftab.on { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
.col-list, .opt-grid { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
.opt-grid { display: grid; grid-template-columns: 1fr 1fr; }
.col-line { display: grid; grid-template-columns: 1fr auto; gap: 6px; align-items: start; }
.col-acts { display: flex; flex-direction: column; gap: 4px; }
.inline { display: flex; gap: 6px; align-items: center; margin-top: 6px; }
.inline input, .inline select { flex: 1; }
.inline select { flex: none; width: 90px; }
.design-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.fields { width: 100%; }
.fields th, .fields td { font-size: 12px; }
.acts { white-space: nowrap; text-align: right; }
.dim { color: var(--text-dim); }
.mono { font-family: var(--mono); }
.pending {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 5px 8px;
  margin-top: 4px;
  background: var(--bg-deep);
  border-radius: var(--radius-sm);
  font-size: 12px;
}
.loading-row { display: flex; justify-content: center; padding: 24px; }
.sql-dock { border-top: 1px solid var(--border-soft); padding: 8px 16px 0; display: flex; flex-direction: column; gap: 6px; flex: none; }
.sql-dock-head { display: flex; align-items: center; justify-content: space-between; font-size: 11px; font-weight: 600; color: var(--text-faint); letter-spacing: 0.06em; }
.sql-pre, .sql-edit {
  margin: 0;
  max-height: 120px;
  overflow: auto;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.5;
  background: var(--bg-deep);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  padding: 8px 10px;
  white-space: pre-wrap;
  word-break: break-all;
}
.sql-pre { color: var(--code-fg); }
.sql-edit { width: 100%; color: var(--text); resize: vertical; }
</style>
