<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { DomainRecord, PriorityDecisionRevision } from '../../types/domain';
import { formatDate, priorityLabel, revisionKindLabel } from '../../utils/format';
import { useAuth } from '../../hooks/useAuth';
import StatusBadge from '../common/StatusBadge.vue';
import SeverityBadge from '../common/SeverityBadge.vue';

const props = defineProps<{ modelValue: boolean; item: DomainRecord | null }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; review: [item: DomainRecord, level: string, reason: string] }>();
const { session, canAtLeast } = useAuth();

const reviewLevel = ref('');
const reviewReason = ref('');

const revisionsDesc = computed(() => [...(props.item?.revisions || [])].reverse());
const isPending = computed(() => props.item?.status === 'review_pending');
const canReview = computed(() =>
  isPending.value && canAtLeast('reviewer') && props.item?.preparedBy !== session.value?.username,
);

watch(() => props.modelValue, (open) => {
  if (open && props.item) {
    reviewLevel.value = props.item.lastFinalLevel || 'observe';
    reviewReason.value = '';
  }
});

function submit() {
  if (!props.item || !reviewLevel.value || reviewReason.value.trim().length < 3) return;
  emit('review', props.item, reviewLevel.value, reviewReason.value.trim());
}

function revisionTone(revision: PriorityDecisionRevision): 'warning' | 'success' | 'primary' | 'info' {
  if (revision.kind === 'reopen') return 'warning';
  if (revision.kind === 'change') return 'primary';
  if (revision.kind === 'maintain' || revision.kind === 'final') return 'success';
  return 'info';
}
</script>

<template>
	<el-dialog
		:model-value="modelValue"
		:title="`优先级决定详情 · ${item?.code ?? ''}`"
		width="min(760px, calc(100vw - 32px))"
		@close="emit('update:modelValue', false)"
	>
		<template v-if="item">
			<section class="detail-head">
				<div>
					<h2>{{ item.name }}</h2>
					<small>{{ item.facility }} · 责任人 {{ item.owner }}</small>
				</div>
				<StatusBadge :status="item.status"/>
			</section>

			<el-alert
				v-if="isPending"
				type="warning"
				show-icon
				:closable="false"
				title="该决定待复核：关联缺陷现场更新后，原结论仅作留档依据"
				class="pending-alert"
			>
				<template #default>
					<p class="pending-reason">{{ item.pendingReason }}</p>
					<p class="pending-meta">
						变化项：<strong>{{ item.pendingChanged }}</strong>
						<span v-if="item.lastFinalLevel">· 原定级 <strong>{{ priorityLabel(item.lastFinalLevel) }}</strong></span>
						<span v-if="item.pendingSince">· 触发时间 {{ formatDate(item.pendingSince) }}</span>
					</p>
				</template>
			</el-alert>

			<section class="detail-grid">
				<div><small>风险等级</small><SeverityBadge :severity="item.riskLevel"/></div>
				<div><small>处置类别</small><strong>{{ item.category }}</strong></div>
				<div><small>关联缺陷</small><strong>{{ item.relatedCode }}</strong></div>
				<div><small>关键指标</small><strong>{{ item.metricValue }} {{ item.metricUnit }}</strong></div>
				<div><small>生效时间</small><strong>{{ formatDate(item.effectiveAt) }}</strong></div>
				<div><small>拟制人</small><strong>{{ item.preparedBy }}</strong></div>
			</section>
			<section class="detail-block">
				<small>处置证据</small>
				<p>{{ item.evidence }}</p>
			</section>

			<section v-if="isPending && canReview" class="review-form">
				<header><strong>复核决定</strong><span>可维持原优先级，也可提交新等级</span></header>
				<el-radio-group v-model="reviewLevel">
					<el-radio-button label="observe">{{ priorityLabel('observe') }}</el-radio-button>
					<el-radio-button label="restrict">{{ priorityLabel('restrict') }}</el-radio-button>
					<el-radio-button label="urgent">{{ priorityLabel('urgent') }}</el-radio-button>
				</el-radio-group>
				<el-input
					v-model="reviewReason"
					type="textarea"
					:rows="3"
					maxlength="500"
					show-word-limit
					placeholder="请填写本次复核依据（至少 3 个字），将随版本永久留档"
				/>
				<p class="review-hint">
					提交基于当前版本 v{{ item.version }}；若他人刚完成复核，系统将拒绝旧版本覆盖并提示刷新。
				</p>
			</section>
			<el-alert
				v-else-if="isPending"
				type="info"
				:closable="false"
				title="待复核决定仅可由非拟制人的复核员/管理员处理"
			/>

			<section class="revision-history">
				<header><strong>历史版本（每次复核依据均留档）</strong><span>共 {{ item.revisions?.length || 0 }} 版</span></header>
				<el-timeline>
					<el-timeline-item
						v-for="revision in revisionsDesc"
						:key="revision.id"
						:timestamp="formatDate(revision.createdAt)"
						:type="revisionTone(revision)"
					>
						<div class="revision-line">
							<el-tag size="small" :type="revisionTone(revision)">{{ revisionKindLabel(revision.kind) }}</el-tag>
							<strong>v{{ revision.version }} · {{ revision.status }}</strong>
							<span class="muted">{{ revision.actor }} · {{ revision.requestId }}</span>
						</div>
						<p class="revision-reason">{{ revision.reason }}</p>
						<p class="revision-evidence">依据：{{ revision.evidence }}</p>
					</el-timeline-item>
				</el-timeline>
			</section>
		</template>
		<template #footer>
			<el-button @click="emit('update:modelValue', false)">关闭</el-button>
			<el-button v-if="isPending && canReview" type="primary" :disabled="!reviewLevel || reviewReason.trim().length < 3" @click="submit">
				提交复核
			</el-button>
		</template>
	</el-dialog>
</template>

<style scoped>
.detail-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; margin-bottom: 14px; }
.detail-head h2 { margin: 0 0 4px; font-size: 19px; }
.detail-head small { color: #768893; }
.pending-alert { margin-bottom: 14px; }
.pending-reason { margin: 0 0 6px; font-weight: 650; }
.pending-meta { margin: 0; color: #526473; font-size: 13px; }
.detail-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px 16px; margin-bottom: 14px; }
.detail-grid small, .detail-block small { display: block; color: #768893; font-size: 12px; margin-bottom: 4px; }
.detail-block { margin-bottom: 14px; }
.detail-block p { margin: 0; line-height: 1.6; }
.review-form { border: 1px solid #e7c87a; background: #fffaf0; padding: 14px; display: grid; gap: 10px; margin-bottom: 16px; }
.review-form > header { display: flex; justify-content: space-between; align-items: baseline; }
.review-form > header span { color: #8a5a08; font-size: 12px; }
.review-hint { margin: 0; color: #8a5a08; font-size: 12px; }
.revision-history > header { display: flex; justify-content: space-between; margin-bottom: 10px; }
.revision-history > header span { color: #768893; font-size: 12px; }
.revision-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.revision-reason { margin: 6px 0 2px; font-weight: 600; }
.revision-evidence { margin: 0; color: #526473; font-size: 13px; }
@media (max-width: 640px) { .detail-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
