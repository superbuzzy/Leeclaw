package promotion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAccessDenied      = errors.New("promotion access denied")
	ErrPromotionBlocked  = errors.New("promotion gate blocked the candidate")
	ErrInvalidTransition = errors.New("invalid promotion state transition")
)

type Service struct {
	Store            Store
	KnowledgeBackend Backend
	SkillBackend     Backend
	Now              func() time.Time
}

func NewService(store Store, knowledgeBackend, skillBackend Backend) *Service {
	return &Service{Store: store, KnowledgeBackend: knowledgeBackend, SkillBackend: skillBackend, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Create(ctx context.Context, p Principal, in CandidateInput) (Candidate, error) {
	if err := validatePrincipal(p); err != nil {
		return Candidate{}, err
	}
	if s.Store == nil {
		return Candidate{}, fmt.Errorf("promotion store is not configured")
	}
	in = normalizeInput(in)
	if err := validateInput(in); err != nil {
		return Candidate{}, err
	}
	if in.Scope == ScopeWorkspace && !roleAtLeast(p.Role, "editor") {
		return Candidate{}, ErrAccessDenied
	}
	id, err := newCandidateID()
	if err != nil {
		return Candidate{}, err
	}
	now := s.now()
	c := Candidate{ID: id, Version: 1, Kind: in.Kind, Scope: in.Scope, State: StateCandidate, WorkspaceID: strings.TrimSpace(p.WorkspaceID), AuthorProfileIDs: []string{strings.TrimSpace(p.UserID)}, SourceSessionIDs: uniqueNonEmpty(in.SourceSessionIDs), SourceExperienceIDs: uniqueNonEmpty(in.SourceExperienceIDs), SourceEvidence: append([]EvidenceLink(nil), in.SourceEvidence...), Knowledge: in.Knowledge, Skill: in.Skill, CreatedAt: now, UpdatedAt: now}
	if err := s.Store.Create(ctx, c); err != nil {
		return Candidate{}, err
	}
	return s.inspectAndPersist(ctx, p, c)
}

func (s *Service) Get(ctx context.Context, p Principal, id string) (Candidate, error) {
	if err := validatePrincipal(p); err != nil {
		return Candidate{}, err
	}
	c, err := s.Store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Candidate{}, err
	}
	if !canRead(p, c) {
		return Candidate{}, ErrAccessDenied
	}
	return c, nil
}

func (s *Service) List(ctx context.Context, p Principal, limit int) ([]Candidate, error) {
	if err := validatePrincipal(p); err != nil {
		return nil, err
	}
	items, err := s.Store.List(ctx, p.WorkspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(items))
	for _, c := range items {
		if canRead(p, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Service) Recheck(ctx context.Context, p Principal, id string) (Candidate, error) {
	c, err := s.Get(ctx, p, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.State == StatePublished || c.State == StateRolledBack || c.State == StateRejected {
		return Candidate{}, ErrInvalidTransition
	}
	if !isAuthor(p.UserID, c) && !roleAtLeast(p.Role, "admin") {
		return Candidate{}, ErrAccessDenied
	}
	return s.inspectAndPersist(ctx, p, c)
}

func (s *Service) Review(ctx context.Context, p Principal, id string, in ReviewInput) (Candidate, error) {
	if !roleAtLeast(p.Role, "admin") {
		return Candidate{}, ErrAccessDenied
	}
	c, err := s.Get(ctx, p, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.State != StateChecked && c.State != StateBlocked {
		return Candidate{}, ErrInvalidTransition
	}
	switch strings.ToLower(strings.TrimSpace(in.Decision)) {
	case "reject", "rejected":
		c.Review = &Review{Decision: "rejected", ReviewerProfileID: p.UserID, Reason: strings.TrimSpace(in.Reason), ReviewedAt: s.now()}
		c.State = StateRejected
	case "approve", "approved":
		if err := promotionGate(c, in); err != nil {
			return Candidate{}, err
		}
		if c.Scope == ScopePersonal {
			if !in.PromoteToWorkspace {
				return Candidate{}, fmt.Errorf("%w: personal candidate must be explicitly promoted to workspace scope before shared publication", ErrPromotionBlocked)
			}
			c.Scope = ScopeWorkspace
		}
		c.Review = &Review{Decision: "approved", ReviewerProfileID: strings.TrimSpace(p.UserID), Reason: strings.TrimSpace(in.Reason), ResolveConflict: in.ResolveConflict, PromoteToWorkspace: in.PromoteToWorkspace, ReviewedAt: s.now()}
		c.State = StateApproved
	default:
		return Candidate{}, fmt.Errorf("review decision must be approve or reject")
	}
	return s.update(ctx, c)
}

func (s *Service) Publish(ctx context.Context, p Principal, id string) (Candidate, error) {
	if !roleAtLeast(p.Role, "admin") {
		return Candidate{}, ErrAccessDenied
	}
	c, err := s.Get(ctx, p, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.State != StateApproved || c.Scope != ScopeWorkspace || c.Review == nil || c.Review.Decision != "approved" {
		return Candidate{}, ErrInvalidTransition
	}
	backend, err := s.backend(c.Kind)
	if err != nil {
		return Candidate{}, err
	}
	latest, err := backend.Inspect(ctx, p, c)
	if err != nil {
		return Candidate{}, fmt.Errorf("pre-publish inspection failed: %w", err)
	}
	if err := validateFreshInspection(c, latest); err != nil {
		return Candidate{}, err
	}
	c.Inspection = &latest
	pub, err := backend.Publish(ctx, p, c)
	if err != nil {
		return Candidate{}, err
	}
	pub.PublishedAt = s.now()
	c.Publication = &pub
	c.State = StatePublished
	return s.update(ctx, c)
}

func (s *Service) Rollback(ctx context.Context, p Principal, id string, in RollbackInput) (Candidate, error) {
	if !roleAtLeast(p.Role, "admin") {
		return Candidate{}, ErrAccessDenied
	}
	c, err := s.Get(ctx, p, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.State != StatePublished || c.Publication == nil {
		return Candidate{}, ErrInvalidTransition
	}
	backend, err := s.backend(c.Kind)
	if err != nil {
		return Candidate{}, err
	}
	if err := backend.Rollback(ctx, p, c); err != nil {
		return Candidate{}, err
	}
	c.State = StateRolledBack
	c.Rollback = &RollbackRecord{RolledBackAt: s.now(), ActorProfileID: p.UserID, Reason: strings.TrimSpace(in.Reason), Result: "restored"}
	return s.update(ctx, c)
}

func (s *Service) inspectAndPersist(ctx context.Context, p Principal, c Candidate) (Candidate, error) {
	backend, err := s.backend(c.Kind)
	if err != nil {
		return Candidate{}, err
	}
	inspection, inspectErr := backend.Inspect(ctx, p, c)
	if inspection.CheckedAt.IsZero() {
		inspection.CheckedAt = s.now()
	}
	if inspectErr != nil {
		inspection.BlockingReason = inspectErr.Error()
	}
	c.Inspection = &inspection
	c.State = StateChecked
	if inspection.Duplicate || !inspection.EvidenceVerified || inspection.BlockingReason != "" || inspection.ValidationStatus == "invalid" {
		c.State = StateBlocked
	}
	return s.update(ctx, c)
}

func (s *Service) update(ctx context.Context, c Candidate) (Candidate, error) {
	expected := c.Version
	c.Version++
	c.UpdatedAt = s.now()
	if err := s.Store.Update(ctx, c, expected); err != nil {
		return Candidate{}, err
	}
	return c, nil
}
func (s *Service) backend(kind CandidateKind) (Backend, error) {
	if kind == KindKnowledge && s.KnowledgeBackend != nil {
		return s.KnowledgeBackend, nil
	}
	if kind == KindSkill && s.SkillBackend != nil {
		return s.SkillBackend, nil
	}
	return nil, fmt.Errorf("promotion backend is not configured for %s", kind)
}
func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func promotionGate(c Candidate, in ReviewInput) error {
	if c.Inspection == nil {
		return fmt.Errorf("%w: candidate has not been inspected", ErrPromotionBlocked)
	}
	i := c.Inspection
	if !validPromotionDecision(i.Decision) {
		return fmt.Errorf("%w: authoritative inspection did not return CREATE, UPDATE, SKIP or CONFLICT", ErrPromotionBlocked)
	}
	if i.Decision == DecisionSkip {
		return fmt.Errorf("%w: inspection decision is SKIP", ErrPromotionBlocked)
	}
	if i.Duplicate {
		return fmt.Errorf("%w: candidate duplicates an existing authoritative asset", ErrPromotionBlocked)
	}
	if !i.EvidenceVerified {
		return fmt.Errorf("%w: source evidence or experience traceability is incomplete", ErrPromotionBlocked)
	}
	if i.BlockingReason != "" || i.ValidationStatus == "invalid" {
		return fmt.Errorf("%w: %s", ErrPromotionBlocked, firstNonEmpty(i.BlockingReason, "validation failed"))
	}
	if i.Conflict && (!in.ResolveConflict || strings.TrimSpace(in.Reason) == "") {
		return fmt.Errorf("%w: conflict requires an explicit reviewer resolution and reason", ErrPromotionBlocked)
	}
	switch c.Kind {
	case KindKnowledge:
		if c.Knowledge == nil || strings.TrimSpace(c.Knowledge.TargetKnowledgeBaseID) == "" {
			return fmt.Errorf("%w: knowledge target is missing", ErrPromotionBlocked)
		}
		if len(c.SourceEvidence) == 0 {
			return fmt.Errorf("%w: knowledge candidate requires evidence", ErrPromotionBlocked)
		}
	case KindSkill:
		if c.Skill == nil {
			return fmt.Errorf("%w: skill candidate is missing", ErrPromotionBlocked)
		}
		if len(c.SourceSessionIDs) == 0 && len(c.SourceExperienceIDs) == 0 {
			return fmt.Errorf("%w: skill candidate requires source session or experience", ErrPromotionBlocked)
		}
		if len(c.Skill.BehaviorDiff) == 0 {
			return fmt.Errorf("%w: skill candidate requires a behavior diff", ErrPromotionBlocked)
		}
		if strings.ToLower(strings.TrimSpace(c.Skill.Eval.Status)) != "passed" {
			return fmt.Errorf("%w: skill regression eval must pass before approval", ErrPromotionBlocked)
		}
	default:
		return fmt.Errorf("%w: unsupported candidate kind", ErrPromotionBlocked)
	}
	return nil
}

func validateFreshInspection(c Candidate, latest Inspection) error {
	if !validPromotionDecision(latest.Decision) {
		return fmt.Errorf("%w: pre-publish inspection returned an invalid decision", ErrPromotionBlocked)
	}
	if latest.Decision == DecisionSkip {
		return fmt.Errorf("%w: pre-publish inspection decision became SKIP", ErrPromotionBlocked)
	}
	if latest.Duplicate {
		return fmt.Errorf("%w: candidate became a duplicate before publication", ErrPromotionBlocked)
	}
	if !latest.EvidenceVerified || latest.BlockingReason != "" || latest.ValidationStatus == "invalid" {
		return fmt.Errorf("%w: pre-publish inspection is no longer valid", ErrPromotionBlocked)
	}
	if latest.Conflict && (c.Review == nil || !c.Review.ResolveConflict) {
		return fmt.Errorf("%w: unresolved conflict appeared before publication", ErrPromotionBlocked)
	}
	if c.Kind == KindSkill && c.Inspection != nil {
		expected := strings.TrimSpace(c.Inspection.CurrentRevision)
		if c.Skill != nil && strings.TrimSpace(c.Skill.ExpectedRevision) != "" {
			expected = strings.TrimSpace(c.Skill.ExpectedRevision)
		}
		if expected != "" && strings.TrimSpace(latest.CurrentRevision) != expected {
			return fmt.Errorf("%w: skill changed after review; candidate must be rechecked", ErrPromotionBlocked)
		}
	}
	return nil
}

func validPromotionDecision(decision PromotionDecision) bool {
	switch decision {
	case DecisionCreate, DecisionUpdate, DecisionSkip, DecisionConflict:
		return true
	default:
		return false
	}
}

func normalizeInput(in CandidateInput) CandidateInput {
	if in.Kind == "" {
		if in.Knowledge != nil && in.Skill == nil {
			in.Kind = KindKnowledge
		} else if in.Skill != nil && in.Knowledge == nil {
			in.Kind = KindSkill
		}
	}
	if in.Scope == "" {
		in.Scope = ScopePersonal
	}
	if in.Skill != nil && strings.TrimSpace(in.Skill.TargetURI) == "" {
		in.Skill.TargetURI = "viking://agent/skills"
	}
	return in
}
func validateInput(in CandidateInput) error {
	if in.Scope != ScopePersonal && in.Scope != ScopeWorkspace {
		return fmt.Errorf("candidate scope must be personal or workspace")
	}
	switch in.Kind {
	case KindKnowledge:
		if in.Knowledge == nil || in.Skill != nil {
			return fmt.Errorf("knowledge candidate requires only knowledge payload")
		}
		if strings.TrimSpace(in.Knowledge.Title) == "" || strings.TrimSpace(in.Knowledge.Statement) == "" || strings.TrimSpace(in.Knowledge.TargetKnowledgeBaseID) == "" {
			return fmt.Errorf("knowledge title, statement and target knowledge base are required")
		}
	case KindSkill:
		if in.Skill == nil || in.Knowledge != nil {
			return fmt.Errorf("skill candidate requires only skill payload")
		}
		if strings.TrimSpace(in.Skill.Name) == "" || strings.TrimSpace(in.Skill.Content) == "" || strings.TrimSpace(in.Skill.Reason) == "" {
			return fmt.Errorf("skill name, content and reason are required")
		}
		if strings.TrimSpace(in.Skill.TargetURI) != "viking://agent/skills" {
			return fmt.Errorf("v0.10 shared skill publication target must be viking://agent/skills")
		}
	default:
		return fmt.Errorf("candidate kind must be knowledge or skill")
	}
	if len(in.SourceSessionIDs) == 0 && len(in.SourceExperienceIDs) == 0 && len(in.SourceEvidence) == 0 {
		return fmt.Errorf("candidate requires at least one traceable source")
	}
	return nil
}
func validatePrincipal(p Principal) error {
	if strings.TrimSpace(p.WorkspaceID) == "" || strings.TrimSpace(p.UserID) == "" || strings.TrimSpace(p.Role) == "" {
		return fmt.Errorf("promotion principal is incomplete")
	}
	return nil
}
func canRead(p Principal, c Candidate) bool {
	if strings.TrimSpace(p.WorkspaceID) != strings.TrimSpace(c.WorkspaceID) {
		return false
	}
	if c.Scope == ScopeWorkspace || roleAtLeast(p.Role, "admin") {
		return true
	}
	return isAuthor(p.UserID, c)
}
func isAuthor(user string, c Candidate) bool {
	user = strings.TrimSpace(user)
	for _, a := range c.AuthorProfileIDs {
		if strings.TrimSpace(a) == user {
			return true
		}
	}
	return false
}
func roleAtLeast(role, minimum string) bool {
	rank := map[string]int{"viewer": 1, "editor": 2, "admin": 3, "owner": 4}
	return rank[strings.ToLower(strings.TrimSpace(role))] >= rank[minimum]
}
func newCandidateID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate candidate id: %w", err)
	}
	return "pc-" + hex.EncodeToString(raw[:]), nil
}
func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
