
export function formatDate(value: string): string {
  return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-';
}
export function nextStatus(current: string, statuses: readonly string[]): string | null {
  const index = statuses.indexOf(current);
  return index >= 0 && index < statuses.length - 1 ? statuses[index + 1] : null;
}

export const PRIORITY_LEVEL_LABELS: Record<string, string> = {
  observe: '观察处置',
  restrict: '限制通行',
  urgent: '立即处置',
};
export function priorityLabel(level: string): string {
  return PRIORITY_LEVEL_LABELS[level] || level;
}

export const REVISION_KIND_LABELS: Record<string, string> = {
  draft: '拟稿',
  final: '定稿',
  reopen: '转入待复核',
  maintain: '复核维持',
  change: '复核改级',
};
export function revisionKindLabel(kind: string): string {
  return REVISION_KIND_LABELS[kind] || kind || '拟稿';
}

const transitions: Record<string, Record<string, readonly string[]>> = {
	bridgeAsset: { active: ['restricted', 'closed'], restricted: ['closed', 'retired', 'active'], closed: ['retired', 'restricted'], retired: ['closed'] },
	inspectionRound: { planned: ['running', 'review'], running: ['review', 'completed', 'planned'], review: ['completed', 'running'], completed: ['review'] },
	defectFinding: { new: ['verified', 'monitoring'], verified: ['monitoring', 'mitigated', 'new'], monitoring: ['mitigated', 'closed', 'verified'], mitigated: ['closed', 'monitoring'], closed: ['mitigated'] },
	// review_pending exposes no generic transition; it is closed only through
	// the dedicated re-review action (keep or change the priority level).
	priorityDecision: { draft: ['observe', 'restrict', 'urgent'], review_pending: [], observe: [], restrict: [], urgent: [] },
};

export function allowedTargets(entityKey: string, current: string): readonly string[] {
	return transitions[entityKey]?.[current] || [];
}
export function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (/approved|accepted|released|completed|signed|closed|pass|ready|online|cleared|succeeded/.test(status)) return 'success';
  if (/failed|rejected|critical|scrap|discard|revoked|urgent/.test(status)) return 'danger';
  if (/hold|warning|review|pending|restricted|limited|quarantine/.test(status)) return 'warning';
  return 'neutral';
}
