<script setup lang="ts">
import { store, toast } from '../stores/app'
import { TableService, errText } from '../lib/api'
import { docModal, closeDocModal } from './docmodal'

async function save() {
  docModal.error = ''
  let doc: any
  try {
    doc = JSON.parse(docModal.docText)
  } catch (e) {
    docModal.error = 'JSON 解析失败:' + (e as Error).message
    return
  }
  if (typeof doc !== 'object' || doc === null || Array.isArray(doc)) {
    docModal.error = '文档必须是 JSON 对象'
    return
  }
  if (docModal.mode === 'replace') {
    delete (doc as any).id
    delete (doc as any)._score
  }
  const connId = docModal.connId || store.activeId
  if (!connId) {
    docModal.error = '未选择连接'
    return
  }
  docModal.saving = true
  try {
    if (docModal.mode === 'insert') {
      const res = await TableService.InsertDocument(connId, docModal.table, docModal.idInput.trim(), doc)
      toast(res?.message ?? '已创建', 'success')
    } else {
      const res = await TableService.ReplaceDocument(connId, docModal.table, docModal.id, doc)
      toast(res?.message ?? '已替换', 'success')
    }
    closeDocModal()
    docModal.onDone?.()
  } catch (e) {
    docModal.error = errText(e)
  } finally {
    docModal.saving = false
  }
}
</script>

<template>
  <div v-if="docModal.open" class="modal-overlay" @click.self="closeDocModal()">
    <div class="modal" style="width: 640px">
      <div class="modal-head">
        <h3>
          {{ docModal.mode === 'insert' ? `插入文档 — ${docModal.table}` : `编辑文档 — ${docModal.table} · id=${docModal.id}` }}
        </h3>
        <button class="modal-close" @click="closeDocModal()">×</button>
      </div>
      <div class="modal-body">
        <div v-if="docModal.mode === 'insert'" style="margin-bottom: 10px">
          <label>文档 id(留空自动生成;percolate 等字符串 id 场景请使用 SQL 控制台)</label>
          <input v-model="docModal.idInput" placeholder="自动" style="width: 220px" />
        </div>
        <label>文档内容(JSON)</label>
        <textarea
          v-model="docModal.docText"
          class="doc-editor"
          spellcheck="false"
        ></textarea>
        <div v-if="docModal.error" class="error-banner" style="margin-top: 10px">{{ docModal.error }}</div>
      </div>
      <div class="modal-foot">
        <span class="flex1"></span>
        <button class="btn" @click="closeDocModal()">取消</button>
        <button class="btn primary" :disabled="docModal.saving" @click="save">
          <span v-if="docModal.saving" class="spinner"></span>
          {{ docModal.mode === 'insert' ? '插入' : '保存 (REPLACE)' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.doc-editor {
  width: 100%;
  height: 320px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.55;
  resize: vertical;
}
.flex1 { flex: 1; }
</style>
