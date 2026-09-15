package promotion

import (
	"context"
	"time"
)

type CandidateKind string

const (
	KindKnowledge CandidateKind = "knowledge"
	KindSkill     CandidateKind = "skill"
)

type CandidateScope string

const (
	ScopePersonal  CandidateScope = "personal"
	ScopeWorkspace CandidateScope = "workspace"
)

type CandidateState string

const (
	StateCandidate  CandidateState = "candidate"
	StateChecked    CandidateState = "checked"
	StateBlocked    CandidateState = "blocked"
	StateApproved   CandidateState = "approved"
	StateRejected   CandidateState = "rejected"
	StatePublished  CandidateState = "published"
	StateRolledBack CandidateState = "rolled_back"
)

// PromotionDecision is the closed set of authoritative-backend outcomes. It
// is separate from the human approve/reject review decision.
type PromotionDecision string

const (
	DecisionCreate   PromotionDecision = "CREATE"
	DecisionUpdate   PromotionDecision = "UPDATE"
	DecisionSkip     PromotionDecision = "SKIP"
	DecisionConflict PromotionDecision = "CONFLICT"
)

type Principal struct {
	WorkspaceID         string
	UserID              string
	Role                string
	TenantID            string
	WeKnoraAPIKey       string
	WeKnoraBaseURL      string
	OpenVikingAccountID string
	KnowledgeBaseIDs    []string
}

type EvidenceLink struct {
	ID              string `json:"id"`
	Source          string `json:"source,omitempty"`
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`
	KnowledgeID     string `json:"knowledge_id,omitempty"`
	ChunkID         string `json:"chunk_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

type EvalResult struct {
	Status      string                 `json:"status"`
	Suite       string                 `json:"suite,omitempty"`
	Score       float64                `json:"score,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

type KnowledgeDraft struct {
	Title                 string     `json:"title"`
	Statement             string     `json:"statement"`
	TargetKnowledgeBaseID string     `json:"target_knowledge_base_id"`
	TargetOntologyID      string     `json:"target_ontology_id,omitempty"`
	ConceptIDs            []string   `json:"concept_ids,omitempty"`
	EntityIDs             []string   `json:"entity_ids,omitempty"`
	ValidFrom             *time.Time `json:"valid_from,omitempty"`
	ValidTo               *time.Time `json:"valid_to,omitempty"`
}

type SkillDraft struct {
	Name               string     `json:"name"`
	Content            string     `json:"content"`
	TargetURI          string     `json:"target_uri,omitempty"`
	Reason             string     `json:"reason"`
	BehaviorDiff       []string   `json:"behavior_diff"`
	AllowedToolsBefore []string   `json:"allowed_tools_before,omitempty"`
	AllowedToolsAfter  []string   `json:"allowed_tools_after,omitempty"`
	ExpectedRevision   string     `json:"expected_revision,omitempty"`
	Eval               EvalResult `json:"eval"`
}

type CandidateInput struct {
	Kind                CandidateKind   `json:"kind"`
	Scope               CandidateScope  `json:"scope,omitempty"`
	SourceSessionIDs    []string        `json:"source_session_ids,omitempty"`
	SourceExperienceIDs []string        `json:"source_experience_ids,omitempty"`
	SourceEvidence      []EvidenceLink  `json:"source_evidence,omitempty"`
	Knowledge           *KnowledgeDraft `json:"knowledge,omitempty"`
	Skill               *SkillDraft     `json:"skill,omitempty"`
}

type Inspection struct {
	CheckedAt           time.Time         `json:"checked_at"`
	Decision            PromotionDecision `json:"decision"`
	EvidenceVerified    bool              `json:"evidence_verified"`
	Duplicate           bool              `json:"duplicate"`
	DuplicateResourceID string            `json:"duplicate_resource_id,omitempty"`
	Conflict            bool              `json:"conflict"`
	ConflictReason      string            `json:"conflict_reason,omitempty"`
	CurrentExists       bool              `json:"current_exists,omitempty"`
	CurrentRevision     string            `json:"current_revision,omitempty"`
	CurrentContent      string            `json:"current_content,omitempty"`
	ValidationStatus    string            `json:"validation_status,omitempty"`
	BlockingReason      string            `json:"blocking_reason,omitempty"`
}

type Review struct {
	Decision           string    `json:"decision"`
	ReviewerProfileID  string    `json:"reviewer_profile_id"`
	Reason             string    `json:"reason,omitempty"`
	ResolveConflict    bool      `json:"resolve_conflict,omitempty"`
	PromoteToWorkspace bool      `json:"promote_to_workspace,omitempty"`
	ReviewedAt         time.Time `json:"reviewed_at"`
}

type RollbackSnapshot struct {
	Action           string `json:"action"`
	ResourceID       string `json:"resource_id,omitempty"`
	TargetURI        string `json:"target_uri,omitempty"`
	PreviousContent  string `json:"previous_content,omitempty"`
	PreviousRevision string `json:"previous_revision,omitempty"`
}

type Publication struct {
	System           string            `json:"system"`
	ResourceID       string            `json:"resource_id"`
	URI              string            `json:"uri,omitempty"`
	Revision         string            `json:"revision,omitempty"`
	Action           string            `json:"action"`
	PublishedAt      time.Time         `json:"published_at"`
	RollbackSnapshot *RollbackSnapshot `json:"rollback_snapshot,omitempty"`
}

type RollbackRecord struct {
	RolledBackAt   time.Time `json:"rolled_back_at"`
	ActorProfileID string    `json:"actor_profile_id"`
	Reason         string    `json:"reason,omitempty"`
	Result         string    `json:"result"`
}

type Candidate struct {
	ID                  string          `json:"id"`
	Version             int64           `json:"version"`
	Kind                CandidateKind   `json:"kind"`
	Scope               CandidateScope  `json:"scope"`
	State               CandidateState  `json:"state"`
	WorkspaceID         string          `json:"workspace_id"`
	AuthorProfileIDs    []string        `json:"author_profile_ids"`
	SourceSessionIDs    []string        `json:"source_session_ids,omitempty"`
	SourceExperienceIDs []string        `json:"source_experience_ids,omitempty"`
	SourceEvidence      []EvidenceLink  `json:"source_evidence,omitempty"`
	Knowledge           *KnowledgeDraft `json:"knowledge,omitempty"`
	Skill               *SkillDraft     `json:"skill,omitempty"`
	Inspection          *Inspection     `json:"inspection,omitempty"`
	Review              *Review         `json:"review,omitempty"`
	Publication         *Publication    `json:"publication,omitempty"`
	Rollback            *RollbackRecord `json:"rollback,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type ReviewInput struct {
	Decision           string `json:"decision"`
	Reason             string `json:"reason,omitempty"`
	ResolveConflict    bool   `json:"resolve_conflict,omitempty"`
	PromoteToWorkspace bool   `json:"promote_to_workspace,omitempty"`
}

type RollbackInput struct {
	Reason string `json:"reason,omitempty"`
}

type Backend interface {
	Inspect(ctx context.Context, principal Principal, candidate Candidate) (Inspection, error)
	Publish(ctx context.Context, principal Principal, candidate Candidate) (Publication, error)
	Rollback(ctx context.Context, principal Principal, candidate Candidate) error
}
