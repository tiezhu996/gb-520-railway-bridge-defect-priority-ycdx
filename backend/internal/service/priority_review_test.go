package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/config"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// finalizeReviewDecision builds draft -> urgent with an independent reviewer
// and returns the finalized aggregate.
func finalizeReviewDecision(t *testing.T, svc PriorityDecisionService, code, related string) model.PriorityDecision {
	t.Helper()
	ctx := context.Background()
	input := priorityCreateInput(code, "evidence-v1")
	input.RelatedCode = related
	created, err := svc.Create(ctx, input, "operator", "req-create")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	finalized, err := svc.Transition(ctx, created.ID, dto.TransitionRequest{
		Status: "urgent", ExpectedVersion: created.Version, Reason: "independent safety review",
	}, "reviewer", model.RoleReviewer, "req-final")
	if err != nil {
		t.Fatalf("finalize decision: %v", err)
	}
	return finalized
}

func TestPriorityDecisionFlaggedWhenLinkedDefectChanges(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	finalized := finalizeReviewDecision(t, svc, "PD-FLAG", "DF-FLAG")

	flagged, err := svc.FlagRelatedForReview(ctx, model.ReviewTrigger{
		DefectID: 9, DefectCode: "DF-FLAG", RiskBefore: "medium", RiskAfter: "critical",
		StateBefore: "verified", StateAfter: "verified",
	}, "operator", "req-flag-risk")
	if err != nil {
		t.Fatalf("flag related decisions: %v", err)
	}
	if flagged != 1 {
		t.Fatalf("expected one flagged decision, got %d", flagged)
	}

	updated, err := svc.Get(ctx, finalized.ID)
	if err != nil {
		t.Fatalf("get flagged decision: %v", err)
	}
	if updated.Status != model.PriorityDecisionPendingReview {
		t.Fatalf("expected pending_review, got %s", updated.Status)
	}
	if updated.LastFinalizedLevel != "urgent" {
		t.Fatalf("last finalized level must stay on record, got %q", updated.LastFinalizedLevel)
	}
	if updated.ReviewReason == "" || updated.ReviewTriggeredAt == nil ||
		updated.RelatedRiskBefore != "medium" || updated.RelatedRiskAfter != "critical" {
		t.Fatalf("review context not persisted: %+v", updated)
	}
	if updated.Version != finalized.Version+1 || len(updated.Revisions) != 3 {
		t.Fatalf("expected new immutable revision, version=%d revisions=%d", updated.Version, len(updated.Revisions))
	}
	flagRevision := updated.Revisions[2]
	if flagRevision.Kind != model.RevisionKindFlagReview || flagRevision.Reason != updated.ReviewReason {
		t.Fatalf("flag revision lost basis: %+v", flagRevision)
	}
}

func TestPriorityDecisionReviewReaffirmAndLevelChange(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	finalized := finalizeReviewDecision(t, svc, "PD-REVIEW", "DF-REVIEW")
	if _, err := svc.FlagRelatedForReview(ctx, model.ReviewTrigger{
		DefectID: 9, DefectCode: "DF-REVIEW",
		StateBefore: "verified", StateAfter: "monitoring",
	}, "operator", "req-flag-state"); err != nil {
		t.Fatalf("flag: %v", err)
	}

	// Transition endpoint must not resolve a pending_review decision.
	stale := dto.TransitionRequest{Status: "observe", ExpectedVersion: finalized.Version + 1, Reason: "wrong endpoint"}
	if _, err := svc.Transition(ctx, finalized.ID, stale, "reviewer", model.RoleReviewer, "req-wrong"); !errors.Is(err, ErrPendingReviewOnly) {
		t.Fatalf("transition on pending_review should fail, got %v", err)
	}

	// A stale expectedVersion must be rejected instead of overwriting.
	if _, err := svc.Review(ctx, finalized.ID, dto.ReviewPriorityDecision{
		ExpectedVersion: finalized.Version, Level: "observe", Reason: "stale client review",
	}, "reviewer", model.RoleReviewer, "req-stale"); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale review must hit optimistic lock, got %v", err)
	}

	reaffirmed, err := svc.Review(ctx, finalized.ID, dto.ReviewPriorityDecision{
		ExpectedVersion: finalized.Version + 1, Level: "urgent", Reason: "现场量测复测仍超限，维持紧急处置",
	}, "reviewer", model.RoleReviewer, "req-reaffirm")
	if err != nil {
		t.Fatalf("reaffirm review: %v", err)
	}
	if reaffirmed.Status != "urgent" || reaffirmed.LastFinalizedLevel != "urgent" || reaffirmed.ReviewReason != "" || reaffirmed.ReviewTriggeredAt != nil {
		t.Fatalf("reaffirmed decision should be cleared of review context: %+v", reaffirmed)
	}
	reaffirmRevision := reaffirmed.Revisions[len(reaffirmed.Revisions)-1]
	if reaffirmRevision.Kind != model.RevisionKindReaffirm {
		t.Fatalf("expected reaffirm revision kind, got %q", reaffirmRevision.Kind)
	}

	// Flag again, then submit a new level.
	if _, err := svc.FlagRelatedForReview(ctx, model.ReviewTrigger{
		DefectID: 9, DefectCode: "DF-REVIEW", RiskBefore: "high", RiskAfter: "low",
		StateBefore: "monitoring", StateAfter: "mitigated",
	}, "operator", "req-flag-again"); err != nil {
		t.Fatalf("re-flag: %v", err)
	}
	changed, err := svc.Review(ctx, finalized.ID, dto.ReviewPriorityDecision{
		ExpectedVersion: reaffirmed.Version + 1, Level: "observe", Reason: "缺陷已缓解，降级为观察",
	}, "reviewer2", model.RoleReviewer, "req-change")
	if err != nil {
		t.Fatalf("level change review: %v", err)
	}
	if changed.Status != "observe" || changed.LastFinalizedLevel != "observe" {
		t.Fatalf("expected new level observe, got %+v", changed)
	}
	last := changed.Revisions[len(changed.Revisions)-1]
	if last.Kind != model.RevisionKindLevelChange {
		t.Fatalf("expected level_change revision kind, got %q", last.Kind)
	}
	// Every review basis remains in history, including the flag reasons.
	if len(changed.Revisions) != 6 {
		t.Fatalf("expected six revisions (draft, final, flag, reaffirm, flag, change), got %d", len(changed.Revisions))
	}
}

func TestPriorityDecisionReviewGuards(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	finalized := finalizeReviewDecision(t, svc, "PD-GUARD", "DF-GUARD")

	// A finalized decision not yet flagged cannot be reviewed.
	if _, err := svc.Review(ctx, finalized.ID, dto.ReviewPriorityDecision{
		ExpectedVersion: finalized.Version, Level: "urgent", Reason: "nothing changed yet",
	}, "reviewer", model.RoleReviewer, "req-early"); !errors.Is(err, ErrNotPendingReview) {
		t.Fatalf("review without flag should fail, got %v", err)
	}
	if _, err := svc.FlagRelatedForReview(ctx, model.ReviewTrigger{
		DefectID: 9, DefectCode: "DF-GUARD", StateBefore: "new", StateAfter: "verified",
	}, "operator", "req-flag"); err != nil {
		t.Fatalf("flag: %v", err)
	}
	// Preparer cannot review their own decision even from the review queue.
	if _, err := svc.Review(ctx, finalized.ID, dto.ReviewPriorityDecision{
		ExpectedVersion: finalized.Version + 1, Level: "urgent", Reason: "self review forbidden",
	}, "operator", model.RoleReviewer, "req-self"); !errors.Is(err, ErrSeparationOfDuty) {
		t.Fatalf("preparer self-review should fail, got %v", err)
	}
}

func TestDefectChangeCascadesToLinkedPriorities(t *testing.T) {
	dsn := fmt.Sprintf("file:defect-cascade-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.PriorityDecision{}, &model.PriorityDecisionRevision{}, &model.DefectFinding{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	prioritySvc := NewPriorityDecisionService(repository.NewPriorityDecisionRepository(db), security)
	defectSvc := NewDefectFindingService(repository.NewDefectFindingRepository(db), security, prioritySvc)
	ctx := context.Background()

	now := time.Now().UTC()
	defect, err := defectSvc.Create(ctx, dto.CreateDefectFinding{
		Code: "DF-CASC", Name: "级联缺陷", Facility: "K42 bridge", Owner: "ops", Category: "structural",
		RiskLevel: "medium", EffectiveAt: now, Evidence: "first inspection",
	}, "operator", "req-defect-create")
	if err != nil {
		t.Fatalf("create defect: %v", err)
	}
	finalizeReviewDecision(t, prioritySvc, "PD-CASC", defect.Code)

	// Risk-level edit should flag the linked decision.
	if _, err := defectSvc.Update(ctx, defect.ID, dto.UpdateDefectFinding{
		ExpectedVersion: defect.Version, Name: defect.Name, Facility: defect.Facility, Owner: defect.Owner,
		Category: defect.Category, RiskLevel: "critical", EffectiveAt: now, Evidence: defect.Evidence,
	}, "operator", "req-defect-update"); err != nil {
		t.Fatalf("update defect risk: %v", err)
	}
	items, err := prioritySvc.List(ctx, dto.PageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list priorities: %v", err)
	}
	var flagged model.PriorityDecision
	for _, item := range items.Items {
		if item.Code == "PD-CASC" {
			flagged = item
		}
	}
	if flagged.Status != model.PriorityDecisionPendingReview || flagged.RelatedRiskBefore != "medium" || flagged.RelatedRiskAfter != "critical" {
		t.Fatalf("linked decision not flagged after risk change: %+v", flagged)
	}
}
