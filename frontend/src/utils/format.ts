
export function formatDate(value: string): string {
  return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-';
}
export function nextStatus(current: string, statuses: readonly string[]): string | null {
  const index = statuses.indexOf(current);
  return index >= 0 && index < statuses.length - 1 ? statuses[index + 1] : null;
}

const transitions: Record<string, Record<string, readonly string[]>> = {
	bridgeAsset: { active: ['restricted', 'closed'], restricted: ['closed', 'retired', 'active'], closed: ['retired', 'restricted'], retired: ['closed'] },
	inspectionRound: { planned: ['running', 'review'], running: ['review', 'completed', 'planned'], review: ['completed', 'running'], completed: ['review'] },
	defectFinding: { new: ['verified', 'monitoring'], verified: ['monitoring', 'mitigated', 'new'], monitoring: ['mitigated', 'closed', 'verified'], mitigated: ['closed', 'monitoring'], closed: ['mitigated'] },
	priorityDecision: { draft: ['observe', 'restrict', 'urgent'], observe: [], restrict: [], urgent: [], pending_review: [] },
};

export function allowedTargets(entityKey: string, current: string): readonly string[] {
	return transitions[entityKey]?.[current] || [];
}

export const PRIORITY_LEVEL_LABELS: Record<string, string> = {
	observe: '观察',
	restrict: '限行',
	urgent: '紧急处置',
};

export const REVISION_KIND_LABELS: Record<string, string> = {
	draft_update: '草稿更新',
	finalize: '定稿',
	flag_review: '标记待复核',
	reaffirm: '维持原等级',
	level_change: '调整等级',
};

export function revisionKindLabel(kind: string): string {
	return REVISION_KIND_LABELS[kind] || kind;
}

export function priorityLevelLabel(level: string): string {
	return PRIORITY_LEVEL_LABELS[level] || level;
}
export function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (/approved|accepted|released|completed|signed|closed|pass|ready|online|cleared|succeeded/.test(status)) return 'success';
  if (/failed|rejected|critical|scrap|discard|revoked|urgent/.test(status)) return 'danger';
  if (/hold|warning|review|pending|restricted|limited|quarantine/.test(status)) return 'warning';
  return 'neutral';
}
