package model

// ReviewTrigger describes why finalized priority decisions linked to a defect
// must be re-reviewed. It is produced by the defect service when a linked
// defect's risk level or disposal state changes and consumed by the priority
// decision service to flag related decisions as pending_review.
type ReviewTrigger struct {
	DefectID    uint
	DefectCode  string
	RiskBefore  string
	RiskAfter   string
	StateBefore string
	StateAfter  string
}

// Changed reports whether the linked defect moved in a way that invalidates a
// finalized priority conclusion.
func (t ReviewTrigger) Changed() bool {
	return t.RiskBefore != t.RiskAfter || t.StateBefore != t.StateAfter
}
