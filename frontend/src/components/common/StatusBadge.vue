
<script setup lang="ts">import { computed } from 'vue'; import { statusTone } from '../../utils/format';
const props = defineProps<{ status: string }>();
const labels: Record<string, string> = {
  draft: '草稿', review_pending: '待复核', observe: '观察处置', restrict: '限制通行', urgent: '立即处置',
  active: '在用', restricted: '受限', closed: '已关闭', retired: '已退役',
  planned: '计划中', running: '进行中', review: '复核中', completed: '已完成',
  new: '新建', verified: '已核实', monitoring: '监测中', mitigated: '已缓解',
};
const tone = computed(() => statusTone(props.status));
const text = computed(() => labels[props.status] || props.status.replaceAll('_', ' '));
</script>
<template><span :class="`status status--${tone}`">{{ text }}</span></template>
