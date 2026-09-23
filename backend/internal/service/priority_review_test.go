package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/config"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// reopenFixture creates a decision, finalizes it as urgent and then flags it
// review_pending via a defect risk change.
func reopenFixture(t *testing.T, ctx context.Context, svc PriorityDecisionService) (string, uint) {
	t.Helper()
	created, err := svc.Create(ctx, priorityCreateInput("PD-RV", "evidence-v1"), "operator", "req-create")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	finalize := dto.TransitionRequest{Status: "urgent", ExpectedVersion: created.Version, Reason: "independent safety review"}
	finalized, err := svc.Transition(ctx, created.ID, finalize, "reviewer", model.RoleReviewer, "req-final")
	if err != nil {
		t.Fatalf("finalize decision: %v", err)
	}
	change := DefectChange{DefectCode: "DF-TEST", RiskBefore: "medium", RiskAfter: "high", StatusBefore: "verified", StatusAfter: "verified"}
	if err := svc.MarkDecisionsForDefectChange(ctx, change, "operator", "req-reopen"); err != nil {
		t.Fatalf("mark review pending: %v", err)
	}
	return finalized.Code, created.ID
}

func TestDefectChangeReopensFinalizedDecision(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	pending, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if pending.Status != constants.PriorityDecisionReviewPending {
		t.Fatalf("expected review_pending, got %q", pending.Status)
	}
	if pending.LastFinalLevel != "urgent" || pending.PendingSince == nil {
		t.Fatalf("pending context not captured: %+v", pending)
	}
	if !strings.Contains(pending.PendingReason, "DF-TEST") || !strings.Contains(pending.PendingChanged, "medium → high") {
		t.Fatalf("pending reason must show change cause, got reason=%q changed=%q", pending.PendingReason, pending.PendingChanged)
	}
	if pending.Version != 3 || len(pending.Revisions) != 3 {
		t.Fatalf("expected reopen revision v3, got version=%d revisions=%d", pending.Version, len(pending.Revisions))
	}
	last := pending.Revisions[2]
	if last.Kind != model.PriorityRevisionReopen || last.Status != "review_pending" || last.Actor != "operator" {
		t.Fatalf("reopen revision misrecorded: %+v", last)
	}
	// The original conclusion must stay archived untouched.
	original := pending.Revisions[1]
	if original.Kind != model.PriorityRevisionFinal || original.Status != "urgent" || original.Reason != "independent safety review" {
		t.Fatalf("original final conclusion altered: %+v", original)
	}
}

func TestReviewerCanMaintainOrChangeLevel(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	maintain := dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "urgent", Reason: "现场已采取限速，复测仍需立即处置"}
	maintained, err := svc.Review(ctx, id, maintain, "reviewer", model.RoleReviewer, "req-maintain")
	if err != nil {
		t.Fatalf("maintain review: %v", err)
	}
	if maintained.Status != "urgent" || maintained.LastFinalLevel != "" || maintained.PendingReason != "" || maintained.PendingSince != nil {
		t.Fatalf("pending context not cleared after maintain: %+v", maintained)
	}
	last := maintained.Revisions[len(maintained.Revisions)-1]
	if last.Kind != model.PriorityRevisionMaintain || last.Actor != "reviewer" || !strings.Contains(last.Reason, "维持原优先级 urgent") {
		t.Fatalf("maintain revision misrecorded: %+v", last)
	}

	// Reopen once more and submit a new level.
	change := DefectChange{DefectCode: "DF-TEST", RiskBefore: "high", RiskAfter: "critical", StatusBefore: "verified", StatusAfter: "monitoring"}
	if err := svc.MarkDecisionsForDefectChange(ctx, change, "operator", "req-reopen-2"); err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	changeLevel := dto.ReviewPriorityDecision{ExpectedVersion: 5, Level: "restrict", Reason: "裂缝扩展放缓，降级为限制通行"}
	changed, err := svc.Review(ctx, id, changeLevel, "admin", model.RoleAdmin, "req-change")
	if err != nil {
		t.Fatalf("change level review: %v", err)
	}
	if changed.Status != "restrict" || changed.Version != 6 {
		t.Fatalf("expected restrict v6, got status=%q version=%d", changed.Status, changed.Version)
	}
	last = changed.Revisions[len(changed.Revisions)-1]
	if last.Kind != model.PriorityRevisionChange || last.Reason != "裂缝扩展放缓，降级为限制通行" {
		t.Fatalf("change revision misrecorded: %+v", last)
	}
	// Every review cycle remains in the history.
	kinds := make([]string, 0, len(changed.Revisions))
	for _, revision := range changed.Revisions {
		kinds = append(kinds, revision.Kind)
	}
	wantKinds := []string{"draft", "final", "reopen", "maintain", "reopen", "change"}
	if fmt.Sprint(kinds) != fmt.Sprint(wantKinds) {
		t.Fatalf("revision evidence chain mismatch: got %v want %v", kinds, wantKinds)
	}
}

func TestStaleReviewVersionIsRejected(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	// Another reviewer finishes the review first (v3 -> v4).
	if _, err := svc.Review(ctx, id, dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "urgent", Reason: "第一位复核员已完成复核"}, "reviewer", model.RoleReviewer, "req-first"); err != nil {
		t.Fatalf("first review: %v", err)
	}
	// A stale browser tab still holds version 3.
	_, err := svc.Review(ctx, id, dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "restrict", Reason: "旧页面覆盖他人复核"}, "reviewer2", model.RoleReviewer, "req-stale")
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale review must be rejected with version conflict, got %v", err)
	}
	current, _ := svc.Get(ctx, id)
	if current.Status != "urgent" {
		t.Fatalf("stale review must not overwrite the finished review, status=%q", current.Status)
	}
}

func TestReviewGuards(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	if _, err := svc.Review(ctx, id, dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "urgent", Reason: "操作员无权复核"}, "operator", model.RoleOperator, "req-role"); !errors.Is(err, ErrReviewRole) {
		t.Fatalf("operator review should fail, got %v", err)
	}
	if _, err := svc.Review(ctx, id, dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "urgent", Reason: "拟制人不得复核自己的决定"}, "operator", model.RoleReviewer, "req-self"); !errors.Is(err, ErrSeparationOfDuty) {
		t.Fatalf("preparer review should fail, got %v", err)
	}
	// Drafts and finalized decisions cannot use the review endpoint.
	created, _ := svc.Create(ctx, priorityCreateInput("PD-DRAFT", "draft evidence"), "operator", "req-create2")
	if _, err := svc.Review(ctx, created.ID, dto.ReviewPriorityDecision{ExpectedVersion: 1, Level: "observe", Reason: "草稿不存在待复核"}, "reviewer", model.RoleReviewer, "req-draft"); !errors.Is(err, ErrNotPendingReview) {
		t.Fatalf("review of draft should fail, got %v", err)
	}
	// Transition endpoint is blocked while pending.
	if _, err := svc.Transition(ctx, id, dto.TransitionRequest{Status: "restrict", ExpectedVersion: 3, Reason: "试图绕过复核环节"}, "reviewer", model.RoleReviewer, "req-bypass"); !errors.Is(err, ErrReviewPending) {
		t.Fatalf("transition while pending should fail, got %v", err)
	}
}

func TestDraftDecisionsAreNotReopened(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, priorityCreateInput("PD-DRAFT2", "draft evidence"), "operator", "req-create"); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	change := DefectChange{DefectCode: "DF-TEST", RiskBefore: "low", RiskAfter: "high", StatusBefore: "new", StatusAfter: "new"}
	if err := svc.MarkDecisionsForDefectChange(ctx, change, "operator", "req-reopen"); err != nil {
		t.Fatalf("mark decisions: %v", err)
	}
	page, err := svc.List(ctx, dto.PageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range page.Items {
		if item.Status == constants.PriorityDecisionReviewPending {
			t.Fatalf("draft decision must never be reopened: %+v", item)
		}
	}
}

func TestRepeatedDefectChangeRefreshesPendingReason(t *testing.T) {
	svc := newPriorityTestService(t)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	second := DefectChange{DefectCode: "DF-TEST", RiskBefore: "high", RiskAfter: "critical", StatusBefore: "verified", StatusAfter: "monitoring"}
	if err := svc.MarkDecisionsForDefectChange(ctx, second, "operator", "req-reopen-2"); err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	pending, _ := svc.Get(ctx, id)
	if pending.Status != constants.PriorityDecisionReviewPending || pending.LastFinalLevel != "urgent" {
		t.Fatalf("repeated change must keep the original final level, got %+v", pending)
	}
	if !strings.Contains(pending.PendingChanged, "critical") || !strings.Contains(pending.PendingChanged, "monitoring") {
		t.Fatalf("pending reason not refreshed: %q", pending.PendingChanged)
	}
	if pending.Version != 4 {
		t.Fatalf("expected version 4 after second reopen, got %d", pending.Version)
	}
}

// flakyReopenRepo fails the first reopen write with a version conflict, then
// delegates, simulating a reviewer committing concurrently with the defect
// change propagation.
type flakyReopenRepo struct {
	repository.PriorityDecisionRepository
	failed bool
}

func (r *flakyReopenRepo) UpdateWithRevision(ctx context.Context, id, version uint, item *model.PriorityDecision, revision *model.PriorityDecisionRevision) error {
	if !r.failed && item.Status == constants.PriorityDecisionReviewPending {
		r.failed = true
		return repository.ErrVersionConflict
	}
	return r.PriorityDecisionRepository.UpdateWithRevision(ctx, id, version, item, revision)
}

func TestReopenRetriesAfterConcurrentConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:priority-retry-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.PriorityDecision{}, &model.PriorityDecisionRevision{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	baseRepo := repository.NewPriorityDecisionRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	flaky := &flakyReopenRepo{PriorityDecisionRepository: baseRepo}
	svc := NewPriorityDecisionService(flaky, security)
	ctx := context.Background()
	_, id := reopenFixture(t, ctx, svc)

	pending, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pending.Status != constants.PriorityDecisionReviewPending {
		t.Fatalf("reopen must survive the first version conflict via retry, got status %q", pending.Status)
	}
	if !flaky.failed {
		t.Fatal("expected the injected conflict path to be exercised")
	}
}

func TestDefectUpdatePropagatesToLinkedDecision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:priority-link-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.PriorityDecision{}, &model.PriorityDecisionRevision{}, &model.DefectFinding{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	prioritySvc := NewPriorityDecisionService(repository.NewPriorityDecisionRepository(db), security)
	defectSvc := NewDefectFindingService(repository.NewDefectFindingRepository(db), security)
	defectSvc.SetChangeNotifier(prioritySvc)
	ctx := context.Background()

	defect, err := defectSvc.Create(ctx, dto.CreateDefectFinding{
		Code: "DF-LINK", Name: "现场缺陷联动", Facility: "K42 bridge", Owner: "field team", Category: "structural",
		RiskLevel: "medium", EffectiveAt: time.Now().UTC(), Evidence: "initial crack photo",
	}, "operator", "req-defect-create")
	if err != nil {
		t.Fatalf("create defect: %v", err)
	}
	decision, err := prioritySvc.Create(ctx, dto.CreatePriorityDecision{
		Code: "PD-LINK", Name: "联动优先级决定", Facility: "K42 bridge", Owner: "field team", Category: "structural",
		RiskLevel: "medium", EffectiveAt: time.Now().UTC(), Evidence: "linked evidence", RelatedCode: "DF-LINK",
	}, "operator", "req-pd-create")
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if _, err := prioritySvc.Transition(ctx, decision.ID, dto.TransitionRequest{Status: "observe", ExpectedVersion: 1, Reason: "独立复核通过观察处置"}, "reviewer", model.RoleReviewer, "req-pd-final"); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Site updates the defect risk: the linked decision must reopen.
	updated, err := defectSvc.Update(ctx, defect.ID, dto.UpdateDefectFinding{
		ExpectedVersion: 1, Name: "现场缺陷联动", Facility: "K42 bridge", Owner: "field team", Category: "structural",
		RiskLevel: "critical", EffectiveAt: time.Now().UTC(), Evidence: "crack widened photo", RelatedCode: "",
	}, "operator", "req-defect-update")
	if err != nil {
		t.Fatalf("update defect: %v", err)
	}
	if updated.RiskLevel != "critical" {
		t.Fatalf("defect risk not updated: %+v", updated)
	}
	reopened, err := prioritySvc.Get(ctx, decision.ID)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if reopened.Status != constants.PriorityDecisionReviewPending {
		t.Fatalf("linked decision should be review_pending after defect risk change, got %q", reopened.Status)
	}

	// A defect status transition reopens it as well. First close the pending cycle.
	if _, err := prioritySvc.Review(ctx, decision.ID, dto.ReviewPriorityDecision{ExpectedVersion: 3, Level: "urgent", Reason: "风险确已升高"}, "reviewer", model.RoleReviewer, "req-review"); err != nil {
		t.Fatalf("review: %v", err)
	}
	if _, err := defectSvc.Transition(ctx, defect.ID, dto.TransitionRequest{Status: "verified", ExpectedVersion: 2, Reason: "现场确认缺陷"}, "operator", "req-defect-transition"); err != nil {
		t.Fatalf("transition defect: %v", err)
	}
	reopened, _ = prioritySvc.Get(ctx, decision.ID)
	if reopened.Status != constants.PriorityDecisionReviewPending {
		t.Fatalf("linked decision should be review_pending after defect status transition, got %q", reopened.Status)
	}
	if !strings.Contains(reopened.PendingChanged, "new → verified") {
		t.Fatalf("status change cause missing: %q", reopened.PendingChanged)
	}
}
