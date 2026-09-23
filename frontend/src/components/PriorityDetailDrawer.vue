<script setup lang="ts">
import { computed } from 'vue';
import type { DomainRecord, PriorityDecisionRevision } from '../types/domain';
import StatusBadge from './common/StatusBadge.vue';
import SeverityBadge from './common/SeverityBadge.vue';
import { formatDate, priorityLevelLabel, revisionKindLabel } from '../utils/format';

const props = defineProps<{ modelValue: boolean; item: DomainRecord | null }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  review: [item: DomainRecord];
}>();

const pendingReview = computed(() => props.item?.status === 'pending_review');
const revisionsDesc = computed<PriorityDecisionRevision[]>(() =>
  props.item ? [...(props.item.revisions || [])].sort((a, b) => b.version - a.version) : []);
</script>

<template>
  <el-drawer :model-value="modelValue" :title="item ? `${item.code} 优先级决定详情` : '优先级决定详情'" size="min(560px, 100%)" @close="emit('update:modelValue', false)">
    <div v-if="item" class="detail">
      <section class="detail-head">
        <div>
          <h2>{{ item.name }}</h2>
          <p>{{ item.facility }} · {{ item.owner }} · {{ item.category }}</p>
        </div>
        <StatusBadge :status="item.status"/>
      </section>

      <el-alert v-if="pendingReview" type="warning" show-icon :closable="false" class="detail-pending">
        <template #title>
          <div class="pending-box">
            <strong>旧结论已留档，等待重新复核</strong>
            <span v-if="item.lastFinalizedLevel">原定稿等级：<strong>{{ priorityLevelLabel(item.lastFinalizedLevel) }}</strong></span>
            <span class="pending-reason">{{ item.reviewReason }}</span>
            <span v-if="item.reviewTriggeredAt">触发时间：{{ formatDate(item.reviewTriggeredAt) }}</span>
          </div>
        </template>
      </el-alert>

      <section class="detail-grid">
        <div><small>风险等级</small><SeverityBadge :severity="item.riskLevel"/></div>
        <div><small>测量指标</small><strong>{{ item.metricValue }} {{ item.metricUnit }}</strong></div>
        <div><small>生效时间</small><strong>{{ formatDate(item.effectiveAt) }}</strong></div>
        <div><small>关联缺陷</small><strong>{{ item.relatedCode || '-' }}</strong></div>
        <div><small>拟制人</small><strong>{{ item.preparedBy }}</strong></div>
        <div><small>当前版本</small><strong>v{{ item.version }}</strong></div>
      </section>

      <section class="detail-block">
        <small>处置证据</small>
        <p>{{ item.evidence }}</p>
      </section>

      <el-button v-if="pendingReview" type="primary" class="detail-review-btn" @click="emit('review', item)">前往复核</el-button>

      <section class="detail-history">
        <h3>历史版本（每次复核依据均已留档）</h3>
        <el-timeline>
          <el-timeline-item v-for="revision in revisionsDesc" :key="revision.id" :timestamp="formatDate(revision.createdAt)" placement="top">
            <article class="revision">
              <header>
                <strong>v{{ revision.version }} · {{ revisionKindLabel(revision.kind) }}</strong>
                <StatusBadge :status="revision.status"/>
              </header>
              <p>{{ revision.reason }}</p>
              <small>{{ revision.actor }} · {{ revision.requestId }}</small>
              <small class="revision-evidence">证据：{{ revision.evidence }}</small>
            </article>
          </el-timeline-item>
        </el-timeline>
      </section>
    </div>
  </el-drawer>
</template>

<style scoped>
.detail { display: grid; gap: 18px; }
.detail-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.detail-head h2 { margin: 0 0 6px; font-size: 20px; }
.detail-head p { margin: 0; color: #667886; font-size: 13px; }
.detail-pending :deep(.el-alert__content) { width: 100%; }
.pending-box { display: grid; gap: 4px; font-weight: 400; }
.pending-reason { font-weight: 700; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; border: 1px solid #dbe4e8; padding: 14px; }
.detail-grid small, .detail-block small { display: block; color: #7d8d97; font-size: 12px; margin-bottom: 4px; }
.detail-block p { margin: 0; line-height: 1.6; }
.detail-review-btn { justify-self: start; }
.detail-history h3 { font-size: 15px; margin: 4px 0 10px; }
.revision { display: grid; gap: 5px; }
.revision header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.revision p { margin: 0; line-height: 1.55; }
.revision small { color: #768893; }
.revision-evidence { overflow-wrap: anywhere; }
</style>
