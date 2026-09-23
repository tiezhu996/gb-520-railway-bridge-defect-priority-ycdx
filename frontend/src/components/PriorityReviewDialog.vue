<script setup lang="ts">
import { ref, watch } from 'vue';
import { ALL_PRIORITY_LEVEL } from '../types/status';
import { priorityLevelLabel } from '../utils/format';
import type { DomainRecord } from '../types/domain';

const props = defineProps<{ modelValue: boolean; item: DomainRecord | null }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  confirm: [payload: { level: string; reason: string }];
}>();

const level = ref('observe');
const reason = ref('');

watch(() => props.modelValue, (open) => {
  if (open) {
    // Default to maintaining the previous conclusion; reviewers may switch level.
    level.value = props.item?.lastFinalizedLevel || 'observe';
    reason.value = '';
  }
});

function submit() {
  if (reason.value.trim().length < 3) return;
  emit('confirm', { level: level.value, reason: reason.value.trim() });
}
</script>

<template>
  <el-dialog :model-value="modelValue" title="优先级复核" width="min(520px, calc(100vw - 32px))" @close="emit('update:modelValue', false)">
    <div v-if="item" class="review-dialog">
      <el-alert type="warning" :closable="false" show-icon>
        <template #title>
          <div class="review-alert">
            <strong>{{ item.code }} 关联缺陷信息已变化</strong>
            <span>{{ item.reviewReason || '关联缺陷的风险等级或处置状态发生变化' }}</span>
            <small v-if="item.lastFinalizedLevel">原定稿等级：{{ priorityLevelLabel(item.lastFinalizedLevel) }}</small>
          </div>
        </template>
      </el-alert>
      <div class="review-field">
        <strong>复核结论</strong>
        <el-radio-group v-model="level" class="review-levels">
          <el-radio-button v-for="candidate in ALL_PRIORITY_LEVEL" :key="candidate" :value="candidate">
            {{ priorityLevelLabel(candidate) }}
          </el-radio-button>
        </el-radio-group>
        <small v-if="item.lastFinalizedLevel">
          选择「{{ priorityLevelLabel(item.lastFinalizedLevel) }}」即维持原优先级，其他选项为提交新等级。
        </small>
      </div>
      <div class="review-field">
        <strong>复核依据 <em>*</em></strong>
        <el-input v-model="reason" type="textarea" :rows="4" maxlength="1000" show-word-limit placeholder="请填写本次复核依据，例如复测数据、现场处置进展或新证据"/>
      </div>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :disabled="reason.trim().length < 3" @click="submit">提交复核</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.review-dialog { display: grid; gap: 16px; }
.review-alert { display: grid; gap: 4px; }
.review-alert span { font-weight: 400; }
.review-alert small { font-weight: 400; color: #8a5a08; }
.review-field { display: grid; gap: 8px; }
.review-field em { color: #c84855; font-style: normal; }
.review-levels { flex-wrap: wrap; }
.review-field > small { color: #7d8d97; }
</style>
