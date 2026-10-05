<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { tableActions, runTableAction } from '../lib/tableops'
import { useSessionId } from '../stores/app'

const props = defineProps<{
  table: string
  floating?: boolean
  x?: number
  y?: number
}>()
const emit = defineEmits<{ close: [] }>()
const connId = useSessionId()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
function onDoc(e: MouseEvent) {
  const target = e.target
  if (!(target instanceof Node)) return
  if (props.floating) {
    if (root.value && !root.value.contains(target)) emit('close')
    return
  }
  if (!open.value) return
  if (root.value && !root.value.contains(target)) open.value = false
}

onMounted(() => document.addEventListener('mousedown', onDoc))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDoc))

async function act(id: string) {
  open.value = false
  emit('close')
  await runTableAction(connId.value, props.table, id)
}
</script>

<template>
  <div
    ref="root"
    class="ops"
    :class="{ floating }"
    :style="floating ? { top: (y ?? 0) + 'px', left: (x ?? 0) + 'px' } : undefined"
    @mousedown.stop
  >
    <button v-if="!floating" class="btn sm" type="button" @click.stop="open = !open">操作</button>
    <div v-if="floating || open" class="ops-menu">
      <template v-for="a in tableActions" :key="a.id">
        <div v-if="a.sep" class="ops-sep"></div>
        <button v-else class="ops-item" :class="{ danger: a.danger }" type="button" @click="act(a.id)">
          {{ a.label }}
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.ops { position: relative; }
.ops.floating { position: fixed; z-index: 90; }
.ops-menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: 40;
  min-width: 188px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-pop);
  padding: 4px;
}
.ops.floating .ops-menu { position: static; }
.ops-item {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  color: var(--text);
  font-size: 12.5px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
}
.ops-item:hover { background: var(--panel-3); }
.ops-item.danger { color: var(--red); }
.ops-item.danger:hover { background: var(--red-dim); }
.ops-sep { height: 1px; background: var(--border-soft); margin: 4px 2px; }
</style>
