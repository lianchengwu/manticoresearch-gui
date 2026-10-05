<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import DataGrid from './DataGrid.vue'
import { closeResult, ops, resolvePrompt } from '../lib/tableops'

const input = ref<HTMLInputElement | null>(null)

watch(() => ops.promptOpen, async (open) => {
  if (!open) return
  await nextTick()
  input.value?.focus()
  input.value?.select()
})

function onKey(e: KeyboardEvent) {
  if (!ops.promptOpen) return
  if (e.key === 'Escape') {
    e.stopPropagation()
    resolvePrompt(false)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    resolvePrompt(true)
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

</script>

<template>
  <div v-if="ops.resultOpen" class="modal-overlay" style="z-index: 160" @click.self="closeResult">
    <div class="modal result-modal">
      <div class="modal-head">
        <h3>{{ ops.resultTitle }}</h3>
        <button class="modal-close" @click="closeResult">×</button>
      </div>
      <div v-if="ops.result?.error" class="modal-body">
        <div class="error-banner">{{ ops.result.error }}</div>
      </div>
      <div v-else class="result-grid">
        <DataGrid :columns="ops.result?.columns ?? []" :rows="ops.result?.rows ?? []" />
      </div>
      <div class="modal-foot">
        <span class="hint">{{ ops.result?.message }}</span>
        <span class="flex1"></span>
        <button class="btn" @click="closeResult">关闭</button>
      </div>
    </div>
  </div>

  <div v-if="ops.promptOpen" class="modal-overlay" style="z-index: 180" @click.self="resolvePrompt(false)" @keydown="onKey">
    <div class="modal" style="width: 420px">
      <div class="modal-head">
        <h3>{{ ops.promptTitle }}</h3>
        <button class="modal-close" @click="resolvePrompt(false)">×</button>
      </div>
      <div class="modal-body">
        <label>{{ ops.promptLabel }}</label>
        <input ref="input" v-model="ops.promptValue" spellcheck="false" @keydown="onKey" />
        <p v-if="ops.promptHint" class="hint" style="margin: 8px 0 0">{{ ops.promptHint }}</p>
      </div>
      <div class="modal-foot">
        <span class="flex1"></span>
        <button class="btn" @click="resolvePrompt(false)">取消</button>
        <button class="btn primary" @click="resolvePrompt(true)">确认</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.flex1 { flex: 1; }
.result-modal {
  width: min(760px, calc(100vw - 32px));
  height: min(520px, 80vh);
}
.result-grid { flex: 1; min-height: 0; display: flex; }
</style>
