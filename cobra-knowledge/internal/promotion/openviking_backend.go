package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OpenVikingBackend struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewOpenVikingBackend(baseURL, apiKey string) *OpenVikingBackend {
	return &OpenVikingBackend{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), APIKey: strings.TrimSpace(apiKey), Client: &http.Client{Timeout: 20 * time.Second}}
}

func (b *OpenVikingBackend) Inspect(ctx context.Context, p Principal, c Candidate) (Inspection, error) {
	i := Inspection{CheckedAt: time.Now().UTC(), Decision: DecisionCreate}
	if c.Kind != KindSkill || c.Skill == nil {
		i.ValidationStatus = "invalid"
		return i, fmt.Errorf("OpenViking backend only accepts skill candidates")
	}
	if err := b.validatePrincipal(p); err != nil {
		i.ValidationStatus = "invalid"
		return i, err
	}
	verified, traceErr := b.verifyTraceability(ctx, p, c)
	i.EvidenceVerified = verified
	if traceErr != nil {
		i.BlockingReason = traceErr.Error()
	}
	validatePayload, err := doJSON(ctx, b.httpClient(), http.MethodPost, endpoint(b.BaseURL, "/api/v1/skills/validate"), b.headers(p), map[string]interface{}{"data": c.Skill.Content, "strict": true, "skill_dir_name": c.Skill.Name, "target_uri": "viking://agent/skills"})
	if err != nil {
		i.ValidationStatus = "invalid"
		i.BlockingReason = "OpenViking skill validation failed: " + err.Error()
		return i, nil
	}
	validation := unwrapObject(validatePayload)
	if valid, ok := validation["valid"].(bool); ok && !valid {
		i.ValidationStatus = "invalid"
		i.BlockingReason = "OpenViking rejected the candidate skill format"
		return i, nil
	}
	i.ValidationStatus = "valid"
	q := url.Values{}
	q.Set("level", "2")
	q.Set("include_content", "true")
	q.Set("include_files", "true")
	q.Set("include_integrity", "true")
	q.Set("target_uri", "viking://agent/skills")
	path := "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(c.Skill.Name)) + "?" + q.Encode()
	payload, err := doJSON(ctx, b.httpClient(), http.MethodGet, endpoint(b.BaseURL, path), b.headers(p), nil)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.StatusCode == http.StatusNotFound {
			if strings.TrimSpace(c.Skill.ExpectedRevision) != "" {
				i.Decision = DecisionConflict
				i.Conflict = true
				i.ConflictReason = "review expected an existing skill revision but the shared skill no longer exists"
			}
			return i, nil
		}
		i.BlockingReason = "read current shared skill: " + err.Error()
		return i, nil
	}
	current := unwrapObject(payload)
	i.Decision = DecisionUpdate
	i.CurrentExists = true
	i.CurrentContent = stringField(current, "content")
	i.CurrentRevision = stringField(current, "revision", "content_sha256")
	if i.CurrentRevision == "" {
		i.CurrentRevision = contentRevision(i.CurrentContent)
	}
	if normalizeText(i.CurrentContent) == normalizeText(c.Skill.Content) {
		i.Decision = DecisionSkip
		i.Duplicate = true
		i.DuplicateResourceID = strings.TrimSpace(c.Skill.Name)
	}
	if expected := strings.TrimSpace(c.Skill.ExpectedRevision); expected != "" && expected != i.CurrentRevision {
		i.Decision = DecisionConflict
		i.Conflict = true
		i.ConflictReason = "shared Skill changed after the candidate was prepared; reviewer must inspect the new base revision"
	}
	return i, nil
}

func (b *OpenVikingBackend) verifyTraceability(ctx context.Context, p Principal, c Candidate) (bool, error) {
	if len(c.SourceSessionIDs) == 0 && len(c.SourceExperienceIDs) == 0 {
		return false, fmt.Errorf("skill candidate requires source session or OpenViking experience URI")
	}
	h := b.headers(p)
	for _, session := range c.SourceSessionIDs {
		session = strings.TrimSpace(session)
		if session == "" {
			continue
		}
		if _, err := doJSON(ctx, b.httpClient(), http.MethodGet, endpoint(b.BaseURL, "/api/v1/sessions/"+url.PathEscape(session)), h, nil); err != nil {
			return false, fmt.Errorf("source session %s is not readable in the current OpenViking principal: %w", session, err)
		}
	}
	for _, exp := range c.SourceExperienceIDs {
		exp = strings.TrimSpace(exp)
		if exp == "" {
			continue
		}
		if !strings.Contains(exp, "/memories/experiences/") {
			return false, fmt.Errorf("source experience must be an OpenViking Experience URI: %s", exp)
		}
		q := url.Values{}
		q.Set("experience_uri", exp)
		q.Set("limit", "1")
		if _, err := doJSON(ctx, b.httpClient(), http.MethodGet, endpoint(b.BaseURL, "/api/v1/agent-evolution/experiences/trajectories?"+q.Encode()), h, nil); err != nil {
			return false, fmt.Errorf("source experience %s is not readable in the current OpenViking principal: %w", exp, err)
		}
	}
	return true, nil
}

func (b *OpenVikingBackend) Publish(ctx context.Context, p Principal, c Candidate) (Publication, error) {
	if c.Skill == nil || c.Inspection == nil {
		return Publication{}, fmt.Errorf("skill candidate or inspection is missing")
	}
	if err := b.validatePrincipal(p); err != nil {
		return Publication{}, err
	}
	name := strings.TrimSpace(c.Skill.Name)
	reviewer := ""
	if c.Review != nil {
		reviewer = strings.TrimSpace(c.Review.ReviewerProfileID)
	}
	meta := map[string]interface{}{"type": "leeclaw_promotion", "candidate_id": c.ID, "workspace_id": c.WorkspaceID, "reviewer": reviewer}
	body := map[string]interface{}{"data": c.Skill.Content, "wait": true, "target_uri": "viking://agent/skills", "source_metadata": meta}
	pub := Publication{System: "openviking", ResourceID: name, URI: "viking://agent/skills/" + name}
	if c.Inspection.CurrentExists {
		payload, err := doJSON(ctx, b.httpClient(), http.MethodPut, endpoint(b.BaseURL, "/api/v1/skills/"+url.PathEscape(name)), b.headers(p), body)
		if err != nil {
			return Publication{}, err
		}
		item := unwrapObject(payload)
		pub.Action = "update"
		pub.Revision = firstNonEmpty(stringField(item, "revision", "content_sha256"), contentRevision(c.Skill.Content))
		pub.RollbackSnapshot = &RollbackSnapshot{Action: "restore_previous_skill", ResourceID: name, TargetURI: "viking://agent/skills", PreviousContent: c.Inspection.CurrentContent, PreviousRevision: c.Inspection.CurrentRevision}
		return pub, nil
	}
	payload, err := doJSON(ctx, b.httpClient(), http.MethodPost, endpoint(b.BaseURL, "/api/v1/skills"), b.headers(p), body)
	if err != nil {
		return Publication{}, err
	}
	item := unwrapObject(payload)
	pub.Action = "create"
	pub.Revision = firstNonEmpty(stringField(item, "revision", "content_sha256"), contentRevision(c.Skill.Content))
	pub.RollbackSnapshot = &RollbackSnapshot{Action: "delete_created_skill", ResourceID: name, TargetURI: "viking://agent/skills"}
	return pub, nil
}
func (b *OpenVikingBackend) Rollback(ctx context.Context, p Principal, c Candidate) error {
	if c.Publication == nil || c.Publication.RollbackSnapshot == nil {
		return fmt.Errorf("skill publication has no rollback snapshot")
	}
	if err := b.validatePrincipal(p); err != nil {
		return err
	}
	snap := c.Publication.RollbackSnapshot
	name := strings.TrimSpace(snap.ResourceID)
	if name == "" {
		return fmt.Errorf("skill rollback resource id is missing")
	}
	switch snap.Action {
	case "restore_previous_skill":
		if strings.TrimSpace(snap.PreviousContent) == "" {
			return fmt.Errorf("skill rollback snapshot has no previous content")
		}
		_, err := doJSON(ctx, b.httpClient(), http.MethodPut, endpoint(b.BaseURL, "/api/v1/skills/"+url.PathEscape(name)), b.headers(p), map[string]interface{}{"data": snap.PreviousContent, "wait": true, "target_uri": "viking://agent/skills", "source_metadata": map[string]interface{}{"type": "leeclaw_promotion_rollback", "candidate_id": c.ID}})
		return err
	case "delete_created_skill":
		q := url.Values{}
		q.Set("target_uri", "viking://agent/skills")
		_, err := doJSON(ctx, b.httpClient(), http.MethodDelete, endpoint(b.BaseURL, "/api/v1/skills/"+url.PathEscape(name)+"?"+q.Encode()), b.headers(p), nil)
		return err
	default:
		return fmt.Errorf("unsupported skill rollback action %q", snap.Action)
	}
}
func (b *OpenVikingBackend) headers(p Principal) http.Header {
	h := make(http.Header)
	h.Set("Accept", "application/json")
	if strings.TrimSpace(b.APIKey) != "" {
		h.Set("Authorization", "Bearer "+strings.TrimSpace(b.APIKey))
	}
	h.Set("X-OpenViking-Account", strings.TrimSpace(p.OpenVikingAccountID))
	h.Set("X-OpenViking-User", strings.TrimSpace(p.UserID))
	return h
}
func (b *OpenVikingBackend) validatePrincipal(p Principal) error {
	if strings.TrimSpace(b.BaseURL) == "" {
		return fmt.Errorf("OpenViking base URL is not configured")
	}
	if strings.TrimSpace(p.OpenVikingAccountID) == "" || strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("OpenViking promotion principal is incomplete")
	}
	return nil
}
func (b *OpenVikingBackend) httpClient() *http.Client {
	if b.Client == nil {
		return &http.Client{Timeout: 20 * time.Second}
	}
	return b.Client
}
func contentRevision(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
