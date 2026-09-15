package promotion

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeBackend struct {
	inspection  Inspection
	publication Publication
	inspectErr  error
	publishErr  error
	rollbackErr error
	published   int
	rolledBack  int
}

func (f *fakeBackend) Inspect(context.Context, Principal, Candidate) (Inspection, error) {
	i := f.inspection
	if i.Decision == "" {
		i.Decision = DecisionCreate
	}
	return i, f.inspectErr
}
func (f *fakeBackend) Publish(context.Context, Principal, Candidate) (Publication, error) {
	f.published++
	return f.publication, f.publishErr
}
func (f *fakeBackend) Rollback(context.Context, Principal, Candidate) error {
	f.rolledBack++
	return f.rollbackErr
}
func testPrincipal(role string) Principal {
	return Principal{WorkspaceID: "w1", UserID: "u1", Role: role, TenantID: "1", WeKnoraAPIKey: "wk", OpenVikingAccountID: "w1", KnowledgeBaseIDs: []string{"kb1"}}
}

func TestKnowledgeCandidateRequiresVerifiedEvidence(t *testing.T) {
	b := &fakeBackend{inspection: Inspection{EvidenceVerified: false, ValidationStatus: "valid"}}
	s := NewService(NewFSStore(t.TempDir()), b, nil)
	c, err := s.Create(context.Background(), testPrincipal("viewer"), CandidateInput{Kind: KindKnowledge, SourceSessionIDs: []string{"s1"}, Knowledge: &KnowledgeDraft{Title: "Rule", Statement: "approved rule", TargetKnowledgeBaseID: "kb1"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateBlocked {
		t.Fatalf("state=%s", c.State)
	}
	_, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve", PromoteToWorkspace: true})
	if !errors.Is(err, ErrPromotionBlocked) {
		t.Fatalf("expected promotion block, got %v", err)
	}
}
func TestPersonalCandidateNeedsExplicitWorkspacePromotion(t *testing.T) {
	b := &fakeBackend{inspection: Inspection{EvidenceVerified: true, ValidationStatus: "valid"}}
	s := NewService(NewFSStore(t.TempDir()), b, nil)
	c, err := s.Create(context.Background(), testPrincipal("viewer"), CandidateInput{Kind: KindKnowledge, SourceEvidence: []EvidenceLink{{ID: "ev1", ChunkID: "c1", KnowledgeBaseID: "kb1"}}, Knowledge: &KnowledgeDraft{Title: "Rule", Statement: "approved rule", TargetKnowledgeBaseID: "kb1"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve"})
	if !errors.Is(err, ErrPromotionBlocked) {
		t.Fatalf("expected explicit workspace promotion block, got %v", err)
	}
	c, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve", PromoteToWorkspace: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateApproved || c.Scope != ScopeWorkspace {
		t.Fatalf("candidate=%+v", c)
	}
}
func TestSkillCandidateRequiresPassedEvalAndDiff(t *testing.T) {
	b := &fakeBackend{inspection: Inspection{EvidenceVerified: true, ValidationStatus: "valid", CurrentExists: true, CurrentRevision: "r1", CurrentContent: "old"}}
	s := NewService(NewFSStore(t.TempDir()), nil, b)
	c, err := s.Create(context.Background(), testPrincipal("editor"), CandidateInput{Kind: KindSkill, Scope: ScopeWorkspace, SourceExperienceIDs: []string{"viking://user/u1/memories/experiences/e1"}, Skill: &SkillDraft{Name: "triage", Content: "new", Reason: "better", BehaviorDiff: []string{"check evidence"}, Eval: EvalResult{Status: "failed"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve"})
	if !errors.Is(err, ErrPromotionBlocked) {
		t.Fatalf("expected eval block, got %v", err)
	}
}
func TestPublishRejectsSkillRevisionRace(t *testing.T) {
	b := &fakeBackend{inspection: Inspection{EvidenceVerified: true, ValidationStatus: "valid", CurrentExists: true, CurrentRevision: "r1", CurrentContent: "old"}}
	s := NewService(NewFSStore(t.TempDir()), nil, b)
	c, err := s.Create(context.Background(), testPrincipal("editor"), CandidateInput{Kind: KindSkill, Scope: ScopeWorkspace, SourceExperienceIDs: []string{"viking://user/u1/memories/experiences/e1"}, Skill: &SkillDraft{Name: "triage", Content: "new", Reason: "better", BehaviorDiff: []string{"check evidence"}, ExpectedRevision: "r1", Eval: EvalResult{Status: "passed"}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	b.inspection.CurrentRevision = "r2"
	_, err = s.Publish(context.Background(), testPrincipal("admin"), c.ID)
	if !errors.Is(err, ErrPromotionBlocked) {
		t.Fatalf("expected revision race block, got %v", err)
	}
	if b.published != 0 {
		t.Fatalf("publish called=%d", b.published)
	}
}
func TestApprovedKnowledgePublishesAndRollsBack(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	b := &fakeBackend{inspection: Inspection{EvidenceVerified: true, ValidationStatus: "valid"}, publication: Publication{System: "weknora", ResourceID: "k1", Action: "create", RollbackSnapshot: &RollbackSnapshot{Action: "delete_created_knowledge", ResourceID: "k1"}}}
	s := NewService(NewFSStore(t.TempDir()), b, nil)
	s.Now = func() time.Time { return now }
	c, err := s.Create(context.Background(), testPrincipal("editor"), CandidateInput{Kind: KindKnowledge, Scope: ScopeWorkspace, SourceEvidence: []EvidenceLink{{ID: "ev1", ChunkID: "c1", KnowledgeBaseID: "kb1"}}, Knowledge: &KnowledgeDraft{Title: "Rule", Statement: "approved rule", TargetKnowledgeBaseID: "kb1"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Publish(context.Background(), testPrincipal("admin"), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StatePublished || b.published != 1 {
		t.Fatalf("candidate=%+v", c)
	}
	c, err = s.Rollback(context.Background(), testPrincipal("admin"), c.ID, RollbackInput{Reason: "regression"})
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateRolledBack || b.rolledBack != 1 {
		t.Fatalf("candidate=%+v", c)
	}
}

func TestSkipDecisionCannotBeApproved(t *testing.T) {
	b := &fakeBackend{inspection: Inspection{Decision: DecisionSkip, EvidenceVerified: true, ValidationStatus: "valid", Duplicate: true}}
	s := NewService(NewFSStore(t.TempDir()), b, nil)
	c, err := s.Create(context.Background(), testPrincipal("editor"), CandidateInput{Kind: KindKnowledge, Scope: ScopeWorkspace, SourceEvidence: []EvidenceLink{{ID: "ev1", ChunkID: "c1", KnowledgeBaseID: "kb1"}}, Knowledge: &KnowledgeDraft{Title: "Rule", Statement: "approved rule", TargetKnowledgeBaseID: "kb1"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateBlocked {
		t.Fatalf("state=%s", c.State)
	}
	_, err = s.Review(context.Background(), testPrincipal("admin"), c.ID, ReviewInput{Decision: "approve"})
	if !errors.Is(err, ErrPromotionBlocked) {
		t.Fatalf("expected SKIP to be blocked, got %v", err)
	}
}
