package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
)

type PriorityDecisionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.PriorityDecision], error)
	Get(context.Context, uint) (model.PriorityDecision, error)
	Create(context.Context, dto.CreatePriorityDecision, string, string) (model.PriorityDecision, error)
	Update(context.Context, uint, dto.UpdatePriorityDecision, string, string, string) (model.PriorityDecision, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.PriorityDecision, error)
	Review(context.Context, uint, dto.ReviewPriorityDecision, string, string, string) (model.PriorityDecision, error)
	FlagRelatedForReview(context.Context, model.ReviewTrigger, string, string) (int, error)
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
	revision, err := newPriorityRevision(item, model.RevisionKindDraft, "decision draft created", actor, requestID)
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
	revision, err := newPriorityRevision(current, model.RevisionKindDraft, "draft business fields updated", actor, requestID)
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
	if role != model.RoleReviewer && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrReviewRole
	}
	if actor == current.PreparedBy {
		return model.PriorityDecision{}, ErrSeparationOfDuty
	}
	if current.Status == model.PriorityDecisionPendingReview {
		return model.PriorityDecision{}, ErrPendingReviewOnly
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.PriorityDecisionTransitions, current.Status, target) {
		return model.PriorityDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.LastFinalizedLevel = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, model.RevisionKindFinalize, strings.TrimSpace(input.Reason), actor, requestID)
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

// Review resolves a pending_review decision: the reviewer reaffirms the last
// finalized level or submits a new one. The optimistic lock means a stale
// ExpectedVersion fails instead of overwriting a review someone else just
// completed. Every resolution appends its basis as an immutable revision.
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
	// Check the optimistic lock before state so a request built against an old
	// version gets a precise conflict even if another reviewer resolved it.
	if current.Version != input.ExpectedVersion {
		return model.PriorityDecision{}, repository.ErrVersionConflict
	}
	if current.Status != model.PriorityDecisionPendingReview {
		return model.PriorityDecision{}, ErrNotPendingReview
	}
	target := strings.TrimSpace(input.Level)
	if !constants.AllPriorityLevelContains(target) {
		return model.PriorityDecision{}, fmt.Errorf("%w: %s", ErrInvalidTransition, target)
	}
	before := model.PriorityDecisionPendingReview
	previousLevel := current.LastFinalizedLevel
	kind := model.RevisionKindReaffirm
	if previousLevel != "" && previousLevel != target {
		kind = model.RevisionKindLevelChange
	}
	reason := fmt.Sprintf("reaffirmed %s: %s", previousLevel, strings.TrimSpace(input.Reason))
	if kind == model.RevisionKindLevelChange {
		reason = fmt.Sprintf("level changed %s -> %s: %s", previousLevel, target, strings.TrimSpace(input.Reason))
	}
	// Clear the pending columns and advance the version; the appended revision
	// keeps the review kind and basis while its snapshot records the new state.
	current.Status = target
	current.LastFinalizedLevel = target
	current.ReviewReason = ""
	current.ReviewTriggeredAt = nil
	current.RelatedRiskBefore = ""
	current.RelatedRiskAfter = ""
	current.RelatedStateBefore = ""
	current.RelatedStateAfter = ""
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

// FlagRelatedForReview keeps finalized conclusions on file but moves every
// finalized decision linked to the changed defect into pending_review,
// recording the change reason. Already-flagged decisions are re-flagged with
// the latest cause and another immutable revision. Each decision uses its own
// optimistic lock, so a decision reviewed concurrently (version advanced) is
// not silently overwritten.
func (s *priorityDecisionService) FlagRelatedForReview(ctx context.Context, trigger model.ReviewTrigger, actor, requestID string) (int, error) {
	if !trigger.Changed() {
		return 0, nil
	}
	relatedCode := strings.ToUpper(strings.TrimSpace(trigger.DefectCode))
	if relatedCode == "" {
		return 0, nil
	}
	decisions, err := s.repository.ListReviewableByRelatedCode(ctx, relatedCode)
	if err != nil {
		return 0, fmt.Errorf("list reviewable 优先级决定: %w", err)
	}
	reason := buildReviewReason(trigger)
	now := time.Now().UTC()
	flagged := 0
	for index := range decisions {
		decision := decisions[index]
		previousLevel := decision.LastFinalizedLevel
		if decision.Status != model.PriorityDecisionPendingReview {
			previousLevel = decision.Status
		}
		before := decision.Status
		decision.Status = model.PriorityDecisionPendingReview
		decision.LastFinalizedLevel = previousLevel
		decision.ReviewReason = reason
		decision.ReviewTriggeredAt = &now
		decision.RelatedRiskBefore = trigger.RiskBefore
		decision.RelatedRiskAfter = trigger.RiskAfter
		decision.RelatedStateBefore = trigger.StateBefore
		decision.RelatedStateAfter = trigger.StateAfter
		decision.Version = decision.Version + 1
		decision.UpdatedAt = now
		revision, err := newPriorityRevision(decision, model.RevisionKindFlagReview, reason, actor, requestID)
		if err != nil {
			return flagged, err
		}
		if err := s.repository.UpdateWithRevision(ctx, decision.ID, decision.Version-1, &decision, &revision); err != nil {
			return flagged, fmt.Errorf("flag 优先级决定 %s for review: %w", decision.Code, err)
		}
		if err := s.security.Audit(ctx, actor, requestID, "flag_review", "PriorityDecision", decision.ID, before, model.PriorityDecisionPendingReview, reason); err != nil {
			return flagged, fmt.Errorf("persist flag review audit: %w", err)
		}
		flagged++
	}
	return flagged, nil
}

func buildReviewReason(trigger model.ReviewTrigger) string {
	var changes []string
	if trigger.RiskBefore != trigger.RiskAfter {
		changes = append(changes, fmt.Sprintf("关联缺陷 %s 风险等级 %s -> %s", trigger.DefectCode, trigger.RiskBefore, trigger.RiskAfter))
	}
	if trigger.StateBefore != trigger.StateAfter {
		changes = append(changes, fmt.Sprintf("关联缺陷 %s 处置状态 %s -> %s", trigger.DefectCode, trigger.StateBefore, trigger.StateAfter))
	}
	return strings.Join(changes, "；")
}

func (s *priorityDecisionService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
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

func newPriorityRevision(item model.PriorityDecision, kind, reason, actor, requestID string) (model.PriorityDecisionRevision, error) {
	item.Revisions = nil
	snapshot, err := json.Marshal(item)
	if err != nil {
		return model.PriorityDecisionRevision{}, fmt.Errorf("serialize priority decision revision: %w", err)
	}
	return model.PriorityDecisionRevision{
		Version: item.Version, Kind: kind, Status: item.Status, Evidence: item.Evidence,
		Reason: strings.TrimSpace(reason), Actor: actor, RequestID: requestID,
		Snapshot: string(snapshot), CreatedAt: time.Now().UTC(),
	}, nil
}
