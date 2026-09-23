
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import type { DomainRecord, EntityConfig, PriorityDecisionRevision } from '../types/domain';
import { allowedTargets, formatDate, priorityLevelLabel } from '../utils/format';
import { PRIORITY_PENDING_REVIEW } from '../types/status';
import { useAuth } from '../hooks/useAuth';
import StatusBadge from './common/StatusBadge.vue';
import SeverityBadge from './common/SeverityBadge.vue';
import EvidenceGallery from './common/EvidenceGallery.vue';
import MetricCard from './common/MetricCard.vue';
import ConfirmDialog from './common/ConfirmDialog.vue';
import PriorityDetailDrawer from './PriorityDetailDrawer.vue';
import PriorityReviewDialog from './PriorityReviewDialog.vue';

const props = withDefaults(defineProps<{ config: EntityConfig; store: any; showEvidence?: boolean }>(), { showEvidence: false });
const { session, canAtLeast } = useAuth();
const search = ref('');
const showCreate = ref(false);
const pending = ref<{ item: DomainRecord; status: string } | null>(null);
const detailItem = ref<DomainRecord | null>(null);
const showDetail = ref(false);
const reviewItem = ref<DomainRecord | null>(null);
const showReview = ref(false);
const canWrite = computed(() => canAtLeast('operator'));
const isPriority = computed(() => props.config.key === 'priorityDecision');
const highRisk = computed(() => props.store.items.filter((item: DomainRecord) => ['high', 'critical'].includes(item.riskLevel)).length);
const pendingReviewCount = computed(() => props.store.items.filter((item: DomainRecord) => item.status === PRIORITY_PENDING_REVIEW).length);

onMounted(() => void props.store.load(props.config.path));

watch(() => props.store.items, syncDetail);

function targetsFor(item: DomainRecord): readonly string[] {
	if (isPriority.value) {
		if (item.status === PRIORITY_PENDING_REVIEW) return [];
		if (!canAtLeast('reviewer') || item.preparedBy === session.value?.username) return [];
	} else if (!canWrite.value) return [];
	return allowedTargets(props.config.key, item.status);
}

function canReview(item: DomainRecord): boolean {
	return isPriority.value && item.status === PRIORITY_PENDING_REVIEW
		&& canAtLeast('reviewer') && item.preparedBy !== session.value?.username;
}

function latestRevision(item: DomainRecord): PriorityDecisionRevision | undefined {
	return item.revisions?.[item.revisions.length - 1];
}

function openDetail(item: DomainRecord) {
	detailItem.value = item;
	showDetail.value = true;
}

function openReview(item: DomainRecord) {
	reviewItem.value = item;
	showReview.value = true;
}

// Keep the open drawer bound to the freshest row after store reloads.
function syncDetail() {
	if (!detailItem.value) return;
	detailItem.value = props.store.items.find((item: DomainRecord) => item.id === detailItem.value?.id) || detailItem.value;
}

async function createDemo() {
	const now = Date.now();
	const created = await props.store.createRecord(props.config.path, {
		code: `${props.config.key.toUpperCase()}-${String(now).slice(-6)}`, name: `新增${props.config.label}`,
		description: '通过前端工作台创建的业务记录', facility: 'K42 桥梁作业区', owner: session.value?.displayName || '现场操作员',
		category: '结构复核', riskLevel: 'medium', metricValue: 25, metricUnit: 'score', effectiveAt: new Date().toISOString(),
		evidence: '现场照片、量测记录与检查批次已完成核对', relatedCode: props.config.key === 'priorityDecision' ? 'DF-001' : 'IR-001',
	});
	if (created) { search.value = ''; showCreate.value = false; }
}

async function confirmTransition() {
	if (!pending.value) return;
	const changed = await props.store.transition(props.config.path, pending.value.item, pending.value.status);
	if (changed) { search.value = ''; pending.value = null; }
}

async function confirmReview(payload: { level: string; reason: string }) {
	if (!reviewItem.value) return;
	const ok = await props.store.reviewPriority(props.config.path, reviewItem.value, payload.level, payload.reason);
	if (ok) {
		showReview.value = false;
		reviewItem.value = null;
		// The drawer stays open and, via the items watcher, reflects the new
		// level plus the appended review revision.
	}
}
</script>

<template>
	<main class="workspace">
		<header class="page-header">
			<div><p class="eyebrow">业务工作台</p><h1>{{ config.label }}</h1><p>统一管理{{ config.label }}的状态、风险、证据与责任人。</p></div>
			<el-button v-if="canWrite" type="primary" @click="showCreate = true">新增{{ config.label }}</el-button>
		</header>
		<section class="metrics">
			<MetricCard label="记录总数" :value="store.meta.total" detail="当前筛选范围"/>
			<MetricCard v-if="isPriority" label="待复核" :value="pendingReviewCount" detail="关联缺陷变化，结论待重新确认"/>
			<MetricCard v-else label="高风险" :value="highRisk" detail="需要优先复核"/>
			<MetricCard label="状态种类" :value="new Set(store.items.map((item: DomainRecord) => item.status)).size" detail="状态机覆盖"/>
		</section>
		<section v-if="showEvidence" class="evidence-panel"><header><strong>证据摘要</strong><span>最近四条记录</span></header><EvidenceGallery :records="store.items"/></section>
		<section class="toolbar"><el-input v-model="search" :placeholder="`搜索${config.label}编码或名称`" clearable/><el-button type="primary" @click="store.load(config.path, search)">查询</el-button><el-button @click="search = ''; store.load(config.path)">重置</el-button></section>
		<el-alert v-if="store.error" :title="store.error" type="error" show-icon/>
		<section class="table-shell">
			<el-table v-loading="store.loading" :data="store.items">
				<el-table-column prop="code" label="编码" width="150"/>
				<el-table-column label="名称" min-width="180"><template #default="{ row }"><strong>{{ row.name }}</strong><small>{{ row.facility }}</small></template></el-table-column>
				<el-table-column label="状态" width="180">
					<template #default="{ row }">
						<div class="status-cell">
							<StatusBadge :status="row.status"/>
							<el-tag v-if="isPriority && row.status === 'pending_review'" type="warning" size="small" effect="plain">
								原定 {{ priorityLevelLabel(row.lastFinalizedLevel) }}
							</el-tag>
							<small v-if="isPriority && row.status === 'pending_review'" class="status-reason">{{ row.reviewReason }}</small>
						</div>
					</template>
				</el-table-column>
				<el-table-column label="风险" width="90"><template #default="{ row }"><SeverityBadge v-if="['defectFinding', 'priorityDecision'].includes(config.key)" :severity="row.riskLevel"/><span v-else>{{ row.riskLevel }}</span></template></el-table-column>
				<el-table-column prop="owner" label="责任人" min-width="130"/>
				<el-table-column label="指标" width="120"><template #default="{ row }">{{ row.metricValue }} {{ row.metricUnit }}</template></el-table-column>
				<el-table-column v-if="isPriority" label="版本审计" width="250"><template #default="{ row }"><strong>v{{ row.version }} · {{ row.preparedBy }}</strong><small>{{ latestRevision(row)?.actor }} · {{ latestRevision(row)?.requestId }}</small><small>{{ latestRevision(row)?.evidence }}</small></template></el-table-column>
				<el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatDate(row.updatedAt) }}</template></el-table-column>
				<el-table-column label="操作" :width="isPriority ? 320 : 300">
					<template #default="{ row }">
						<div class="row-actions">
							<el-button v-if="isPriority" link type="info" @click="openDetail(row)">详情</el-button>
							<el-button v-if="canReview(row)" link type="warning" @click="openReview(row)">待复核</el-button>
							<el-button v-for="target in targetsFor(row)" :key="target" link type="primary" @click="pending = { item: row, status: target }">推进至 {{ target }}</el-button>
							<span v-if="targetsFor(row).length === 0 && !canReview(row) && !isPriority" class="muted">无可用操作</span>
						</div>
					</template>
				</el-table-column>
			</el-table>
		</section>
		<ConfirmDialog v-model="showCreate" :title="`新增${config.label}`" @confirm="createDemo"><p>将创建一条包含完整责任人、风险和证据信息的记录。</p></ConfirmDialog>
		<ConfirmDialog :model-value="Boolean(pending)" title="确认状态迁移" @update:model-value="pending = null" @confirm="confirmTransition"><p>状态迁移会写入审计日志并保留请求号；优先级定稿后不可覆盖。</p><strong>{{ pending?.item.status }} → {{ pending?.status }}</strong></ConfirmDialog>
		<PriorityDetailDrawer v-if="isPriority" v-model="showDetail" :item="detailItem" @review="openReview"/>
		<PriorityReviewDialog v-model="showReview" :item="reviewItem" @confirm="confirmReview"/>
	</main>
</template>

<style scoped>
.status-cell { display: grid; gap: 6px; justify-items: start; }
.status-reason { color: #8a5a08; font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
</style>
