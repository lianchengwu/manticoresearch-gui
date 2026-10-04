<script setup lang="ts">
import { store } from '../stores/app'
</script>

<template>
  <div class="toasts">
    <transition-group name="toast">
      <div v-for="t in store.toasts" :key="t.id" class="toast" :class="t.type">
        <span v-if="t.type === 'success'">✓</span>
        <span v-else-if="t.type === 'error'">✕</span>
        <span v-else>ℹ</span>
        <span class="toast-text">{{ t.text }}</span>
      </div>
    </transition-group>
  </div>
</template>

<style scoped>
.toasts {
  position: fixed;
  right: 16px;
  bottom: calc(var(--statusbar-h) + 14px);
  display: flex;
  flex-direction: column;
  gap: 8px;
  z-index: 300;
  pointer-events: none;
}
.toast {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 240px;
  max-width: 460px;
  padding: 9px 13px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--border);
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.45);
  font-size: 12.5px;
  word-break: break-all;
}
.toast.success { border-left: 3px solid var(--green); }
.toast.error { border-left: 3px solid var(--red); }
.toast.info { border-left: 3px solid var(--accent); }
.toast.success > span:first-child { color: var(--green); }
.toast.error > span:first-child { color: var(--red); }
.toast.info > span:first-child { color: var(--accent); }
.toast-text { user-select: text; }

.toast-enter-active, .toast-leave-active { transition: all 0.2s ease; }
.toast-enter-from { opacity: 0; transform: translateY(8px); }
.toast-leave-to { opacity: 0; transform: translateX(20px); }
</style>
