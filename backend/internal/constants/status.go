package constants

// Shared status values are mirrored in frontend/src/types/status.ts. Keeping
// the lists explicit makes state-machine drift visible during code review.

type DefectState string

const (
	DefectStateNew        DefectState = "new"
	DefectStateVerified   DefectState = "verified"
	DefectStateMonitoring DefectState = "monitoring"
	DefectStateMitigated  DefectState = "mitigated"
	DefectStateClosed     DefectState = "closed"
)

var AllDefectState = []string{"new", "verified", "monitoring", "mitigated", "closed"}

type PriorityLevel string

const (
	PriorityLevelObserve  PriorityLevel = "observe"
	PriorityLevelRestrict PriorityLevel = "restrict"
	PriorityLevelUrgent   PriorityLevel = "urgent"
)

var AllPriorityLevel = []string{"observe", "restrict", "urgent"}

var BridgeAssetTransitions = map[string]map[string]bool{
	"active":     {"restricted": true, "closed": true},
	"restricted": {"closed": true, "retired": true, "active": true},
	"closed":     {"retired": true, "restricted": true},
	"retired":    {"closed": true},
}

var InspectionRoundTransitions = map[string]map[string]bool{
	"planned":   {"running": true, "review": true},
	"running":   {"review": true, "completed": true, "planned": true},
	"review":    {"completed": true, "running": true},
	"completed": {"review": true},
}

var DefectFindingTransitions = map[string]map[string]bool{
	"new":        {"verified": true, "monitoring": true},
	"verified":   {"monitoring": true, "mitigated": true, "new": true},
	"monitoring": {"mitigated": true, "closed": true, "verified": true},
	"mitigated":  {"closed": true, "monitoring": true},
	"closed":     {"mitigated": true},
}

// PriorityDecisionTransitions only models externally requested moves. Decisions
// are reopened to review_pending by the system when the linked defect changes,
// never by a direct transition call, so review_pending stays out of this graph.
var PriorityDecisionTransitions = map[string]map[string]bool{
	"draft":    {"observe": true, "restrict": true, "urgent": true},
	"observe":  {},
	"restrict": {},
	"urgent":   {},
}

// PriorityDecisionReopenTransitions is the system-driven graph: a finalized
// decision is flagged review_pending, and a reviewer re-confirms one of the
// three priority levels. review_pending loops back to itself on purpose: a
// second defect change while a review is still pending refreshes the reason
// without leaving the pending state.
var PriorityDecisionReopenTransitions = map[string]map[string]bool{
	"observe":        {"review_pending": true},
	"restrict":       {"review_pending": true},
	"urgent":         {"review_pending": true},
	"review_pending": {"review_pending": true, "observe": true, "restrict": true, "urgent": true},
}

const PriorityDecisionReviewPending = "review_pending"

func CanTransition(graph map[string]map[string]bool, from, to string) bool {
	targets, exists := graph[from]
	return exists && targets[to]
}
