package dto

import "time"

// CreatePriorityDecision is the public write contract for 优先级决定. Status is deliberately
// omitted so callers cannot bypass the service state machine.
type CreatePriorityDecision struct {
	Code        string    `json:"code" binding:"required,min=2,max=64"`
	Name        string    `json:"name" binding:"required,min=2,max=160"`
	Description string    `json:"description" binding:"max=1000"`
	Facility    string    `json:"facility" binding:"required,max=120"`
	Owner       string    `json:"owner" binding:"required,max=120"`
	Category    string    `json:"category" binding:"required,max=80"`
	RiskLevel   string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt time.Time `json:"effectiveAt" binding:"required"`
	Evidence    string    `json:"evidence" binding:"required,max=2000"`
	RelatedCode string    `json:"relatedCode" binding:"required,max=64"`
}

type UpdatePriorityDecision struct {
	ExpectedVersion uint      `json:"expectedVersion" binding:"required"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"required,max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"required,max=64"`
}

// ReviewPriorityDecision re-evaluates a decision flagged review_pending. The
// reviewer either confirms the previous level (expectedVersion still required
// to reject stale submissions) or submits a new level with fresh rationale.
type ReviewPriorityDecision struct {
	ExpectedVersion uint   `json:"expectedVersion" binding:"required"`
	Level           string `json:"level" binding:"required,oneof=observe restrict urgent"`
	Reason          string `json:"reason" binding:"required,min=3,max=500"`
}
