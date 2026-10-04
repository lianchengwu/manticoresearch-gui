<script setup lang="ts">
import { store, resolveConfirm } from '../stores/app'
</script>

<template>
  <div v-if="store.confirm.open" class="modal-overlay" style="z-index: 200" @click.self="resolveConfirm(false)">
    <div class="modal" style="width: 420px">
      <div class="modal-head">
        <h3>{{ store.confirm.title }}</h3>
        <button class="modal-close" @click="resolveConfirm(false)">×</button>
      </div>
      <div class="modal-body">{{ store.confirm.body }}</div>
      <div class="modal-foot">
        <span class="flex1"></span>
        <button class="btn" @click="resolveConfirm(false)">取消</button>
        <button class="btn primary" :class="{ danger: store.confirm.danger }" @click="resolveConfirm(true)">
          确认
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.flex1 { flex: 1; }
.btn.primary.danger {
  background: var(--red);
  border-color: var(--red);
}
.btn.primary.danger:hover { background: #ff7386; }
</style>
