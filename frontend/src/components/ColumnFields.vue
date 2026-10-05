<script setup lang="ts">
import { COLUMN_TYPES, type ColumnDraft } from '../lib/schema'

defineProps<{ col: ColumnDraft }>()
</script>

<template>
  <div class="col-card">
    <div class="col-top">
      <input v-model="col.name" placeholder="字段名" spellcheck="false" />
      <select v-model="col.type">
        <option v-for="[id, label] in COLUMN_TYPES" :key="id" :value="id">{{ label }}</option>
      </select>
      <select v-model="col.engine" title="属性存储引擎">
        <option value="">默认存储</option>
        <option value="columnar">columnar</option>
        <option value="rowwise">rowwise</option>
      </select>
    </div>
    <div v-if="col.type === 'text'" class="col-flags">
      <label class="chk"><input v-model="col.indexed" type="checkbox" />indexed 全文索引</label>
      <label class="chk"><input v-model="col.stored" type="checkbox" />stored 存储原文</label>
      <label class="chk"><input v-model="col.attribute" type="checkbox" />attribute 字符串属性</label>
    </div>
    <div v-else-if="col.type === 'json'" class="col-flags">
      <label class="chk"><input v-model="col.secondaryIndex" type="checkbox" />secondary_index 二级索引</label>
    </div>
    <div v-else-if="col.type === 'float_vector'" class="col-flags vec">
      <label>knn_type
        <select v-model="col.knnType">
          <option value="">无</option>
          <option value="hnsw">hnsw</option>
        </select>
      </label>
      <label>dims <input v-model.number="col.knnDims" type="number" min="1" placeholder="维度" /></label>
      <label>similarity
        <select v-model="col.hnswSimilarity">
          <option value="">默认</option>
          <option value="cosine">cosine</option>
          <option value="l2">l2</option>
          <option value="ip">ip</option>
        </select>
      </label>
    </div>
  </div>
</template>

<style scoped>
.col-card {
  background: var(--bg-deep);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.col-top { display: grid; grid-template-columns: 1.3fr 140px 120px; gap: 6px; }
.col-flags { display: flex; flex-wrap: wrap; gap: 10px 14px; align-items: center; }
.chk {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin: 0;
  color: var(--text-dim);
  font-size: 11.5px;
  cursor: pointer;
}
.vec { display: grid; grid-template-columns: 1fr 90px 1fr; gap: 6px; width: 100%; }
.vec label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin: 0;
  font-size: 11px;
}
</style>
