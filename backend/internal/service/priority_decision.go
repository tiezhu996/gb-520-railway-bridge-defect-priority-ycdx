package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
)

// DefectChange describes the on-site change that may invalidate finalized
// priority decisions. Empty before/after pairs mean that dimension did not
// change.
type DefectChange struct {
	DefectCode   string
	RiskBefore   string
	RiskAfter    string
	StatusBefore string
	StatusAfter  string
}

type PriorityDecisionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.PriorityDecision], error)
	Get(context.Context, uint) (model.PriorityDecision, error)
	Create(context.Context, dto.CreatePriorityDecision, string, string) (model.PriorityDecision, error)
	Update(context.Context, uint, dto.UpdatePriorityDecision, string, string, string) (model.PriorityDecision, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.PriorityDecision, error)
	Review(context.Context, uint, dto.ReviewPriorityDecision, string, string, string) (model.PriorityDecision, error)
	MarkDecisionsForDefectChange(context.Context, DefectChange, string, string) error
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type priorityDecisionService struct {
	repository repository.PriorityDecisionRepository
	security   SecurityService
}

func NewPriorityDecisionService(repo repository.PriorityDecisionRepository, security SecurityService) PriorityDecisionService {
	return &priorityDecisionService{repository: repo, security: security}
}

func (s *priorityDecisionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.PriorityDecision], error) {
	return s.repository.List(ctx, query)
}

func (s *priorityDecisionService) Get(ctx context.Context, id uint) (model.PriorityDecision, error) {
	return s.repository.Get(ctx, id)
}

func (s *priorityDecisionService) Create(ctx context.Context, input dto.CreatePriorityDecision, actor, requestID string) (model.PriorityDecision, error) {
	if err := validatePriorityDecisionBusinessFields(input.Code, input.Name, input.Facility, input.Owner, input.Evidence, input.RelatedCode); err != nil {
		return model.PriorityDecision{}, err
	}
	item := model.PriorityDecision{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.PriorityDecisionInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		PreparedBy:  actor,
	}
	revision, err := newPriorityRevision(item, model.PriorityRevisionDraft, "decision draft created", actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.CreateWithRevision(ctx, &item, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("create 优先级决定: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "PriorityDecision", item.ID, "", item.Status, "created 优先级决定")
	return s.repository.Get(ctx, item.ID)
}

func (s *priorityDecisionService) Update(ctx context.Context, id uint, input dto.UpdatePriorityDecision, actor, role, requestID string) (model.PriorityDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if current.Status == constants.PriorityDecisionReviewPending {
		return model.PriorityDecision{}, ErrReviewPending
	}
	if current.Status != model.PriorityDecisionInitialStatus {
		return model.PriorityDecision{}, ErrDecisionLocked
	}
	if actor != current.PreparedBy && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrNotDecisionOwner
	}
	if err := validatePriorityDecisionBusinessFields(current.Code, input.Name, input.Facility, input.Owner, input.Evidence, input.RelatedCode); err != nil {
		return model.PriorityDecision{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, model.PriorityRevisionDraft, "draft business fields updated", actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("update 优先级决定: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "PriorityDecision", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *priorityDecisionService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.PriorityDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if current.Status == constants.PriorityDecisionReviewPending {
		return model.PriorityDecision{}, ErrReviewPending
	}
	if role != model.RoleReviewer && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrReviewRole
	}
	if actor == current.PreparedBy {
		return model.PriorityDecision{}, ErrSeparationOfDuty
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.PriorityDecisionTransitions, current.Status, target) {
		return model.PriorityDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, model.PriorityRevisionFinal, strings.TrimSpace(input.Reason), actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("transition 优先级决定: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "PriorityDecision", id, before, target, input.Reason); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

// Review closes a review_pending cycle. The reviewer either keeps the previous
// level or picks a new one. expectedVersion is checked against the latest
// revision so a stale form (read before another reviewer finished) is rejected.
func (s *priorityDecisionService) Review(ctx context.Context, id uint, input dto.ReviewPriorityDecision, actor, role, requestID string) (model.PriorityDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if role != model.RoleReviewer && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrReviewRole
	}
	if actor == current.PreparedBy {
		return model.PriorityDecision{}, ErrSeparationOfDuty
	}
	// Reject stale submissions before anything else so an old browser tab can
	// never overwrite a review a colleague just finished.
	if input.ExpectedVersion != current.Version {
		return model.PriorityDecision{}, repository.ErrVersionConflict
	}
	if current.Status != constants.PriorityDecisionReviewPending {
		return model.PriorityDecision{}, ErrNotPendingReview
	}
	target := strings.TrimSpace(input.Level)
	if !constants.CanTransition(constants.PriorityDecisionReopenTransitions, current.Status, target) {
		return model.PriorityDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	kind := model.PriorityRevisionMaintain
	reason := strings.TrimSpace(input.Reason)
	if target != current.LastFinalLevel {
		kind = model.PriorityRevisionChange
	} else {
		reason = fmt.Sprintf("复核后维持原优先级 %s：%s", target, reason)
	}
	before := current.Status
	current.Status = target
	current.LastFinalLevel = ""
	current.PendingReason = ""
	current.PendingChanged = ""
	current.PendingSince = nil
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, kind, reason, actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("review 优先级决定: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "review", "PriorityDecision", id, before, target, reason); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("persist review audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

// MarkDecisionsForDefectChange flags every finalized decision linked to the
// changed defect as review_pending. The previous conclusion stays untouched in
// the append-only revisions; a new reopen revision records why it was flagged.
// Decisions already pending get their reason refreshed instead of being
// reopened again. Drafts are ignored because they never carried a conclusion.
func (s *priorityDecisionService) MarkDecisionsForDefectChange(ctx context.Context, change DefectChange, actor, requestID string) error {
	parts := make([]string, 0, 2)
	if change.RiskBefore != change.RiskAfter && change.RiskAfter != "" {
		parts = append(parts, fmt.Sprintf("风险等级 %s → %s", change.RiskBefore, change.RiskAfter))
	}
	if change.StatusBefore != change.StatusAfter && change.StatusAfter != "" {
		parts = append(parts, fmt.Sprintf("处置状态 %s → %s", change.StatusBefore, change.StatusAfter))
	}
	if len(parts) == 0 || strings.TrimSpace(change.DefectCode) == "" {
		return nil
	}
	changed := strings.Join(parts, "；")
	reason := truncateText(fmt.Sprintf("关联缺陷 %s 现场更新，%s，原结论转入待复核", change.DefectCode, changed), 480)

	decisions, err := s.repository.ListFinalizedByRelatedCode(ctx, change.DefectCode)
	if err != nil {
		return fmt.Errorf("lookup decisions linked to %s: %w", change.DefectCode, err)
	}
	for index := range decisions {
		if err := s.reopenDecision(ctx, decisions[index].ID, reason, changed, actor, requestID); err != nil {
			return err
		}
	}
	return nil
}

// reopenDecision flags one decision with bounded retries. A conflict means a
// reviewer (or a second defect update) committed in between; reloading ensures
// the on-site change still produces a review_pending flag instead of being
// silently lost.
func (s *priorityDecisionService) reopenDecision(ctx context.Context, decisionID uint, reason, changed, actor, requestID string) error {
	for attempt := 0; attempt < 3; attempt++ {
		decision, err := s.repository.Get(ctx, decisionID)
		if err != nil {
			return err
		}
		if !constants.CanTransition(constants.PriorityDecisionReopenTransitions, decision.Status, constants.PriorityDecisionReviewPending) {
			return nil
		}
		expected := decision.Version
		firstReopen := decision.Status != constants.PriorityDecisionReviewPending
		before := decision.Status
		if firstReopen {
			decision.LastFinalLevel = before
			now := time.Now().UTC()
			decision.PendingSince = &now
		}
		decision.Status = constants.PriorityDecisionReviewPending
		decision.PendingReason = reason
		decision.PendingChanged = truncateText(changed, 190)
		decision.Version = expected + 1
		decision.UpdatedAt = time.Now().UTC()
		revision, err := newPriorityRevision(decision, model.PriorityRevisionReopen, reason, actor, requestID)
		if err != nil {
			return err
		}
		err = s.repository.UpdateWithRevision(ctx, decision.ID, expected, &decision, &revision)
		if errors.Is(err, repository.ErrVersionConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("flag decision %s for re-review: %w", decision.Code, err)
		}
		if err := s.security.Audit(ctx, actor, requestID, "reopen", "PriorityDecision", decision.ID, before, constants.PriorityDecisionReviewPending, reason); err != nil {
			return fmt.Errorf("persist reopen audit: %w", err)
		}
		return nil
	}
	return fmt.Errorf("flag decision %d for re-review: %w", decisionID, repository.ErrVersionConflict)
}

func (s *priorityDecisionService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status == constants.PriorityDecisionReviewPending {
		return ErrReviewPending
	}
	if current.Status != model.PriorityDecisionInitialStatus {
		return ErrDecisionLocked
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "PriorityDecision", id, current.Status, "deleted", "soft deleted 优先级决定")
}

func (s *priorityDecisionService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validatePriorityDecisionBusinessFields(code, name, facility, owner, evidence, relatedCode string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(evidence) == "" || strings.TrimSpace(relatedCode) == "" {
		return ErrInvalidInput
	}
	return nil
}

func truncateText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func newPriorityRevision(item model.PriorityDecision, kind, reason, actor, requestID string) (model.PriorityDecisionRevision, error) {
	item.Revisions = nil
	snapshot, err := json.Marshal(item)
	if err != nil {
		return model.PriorityDecisionRevision{}, fmt.Errorf("serialize priority decision revision: %w", err)
	}
	return model.PriorityDecisionRevision{
		Kind: kind, Version: item.Version, Status: item.Status, Evidence: item.Evidence,
		Reason: strings.TrimSpace(reason), Actor: actor, RequestID: requestID,
		Snapshot: string(snapshot), CreatedAt: time.Now().UTC(),
	}, nil
}
