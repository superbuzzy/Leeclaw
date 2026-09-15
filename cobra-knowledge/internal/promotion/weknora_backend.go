package promotion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WeKnoraBackend struct {
	DefaultBaseURL     string
	DuplicateThreshold float64
	ConflictThreshold  float64
	Client             *http.Client
}

func NewWeKnoraBackend(baseURL string) *WeKnoraBackend {
	return &WeKnoraBackend{DefaultBaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), DuplicateThreshold: .985, ConflictThreshold: .90, Client: &http.Client{Timeout: 20 * time.Second}}
}

func (b *WeKnoraBackend) Inspect(ctx context.Context, p Principal, c Candidate) (Inspection, error) {
	i := Inspection{CheckedAt: time.Now().UTC(), Decision: DecisionCreate, ValidationStatus: "valid"}
	if c.Kind != KindKnowledge || c.Knowledge == nil {
		i.ValidationStatus = "invalid"
		return i, fmt.Errorf("WeKnora backend only accepts knowledge candidates")
	}
	base, err := b.baseURL(p)
	if err != nil {
		i.ValidationStatus = "invalid"
		return i, err
	}
	if strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.WeKnoraAPIKey) == "" || strings.TrimSpace(p.UserID) == "" {
		i.ValidationStatus = "invalid"
		return i, fmt.Errorf("WeKnora promotion principal is incomplete")
	}
	target := strings.TrimSpace(c.Knowledge.TargetKnowledgeBaseID)
	if !knowledgeBaseAllowed(p.KnowledgeBaseIDs, target) {
		i.ValidationStatus = "invalid"
		return i, fmt.Errorf("target knowledge base is outside the workspace scope")
	}
	verified, err := b.verifyEvidence(ctx, base, p, c.SourceEvidence)
	if err != nil {
		i.BlockingReason = err.Error()
		return i, nil
	}
	i.EvidenceVerified = verified
	if !verified {
		i.BlockingReason = "knowledge candidate requires at least one verifiable WeKnora chunk evidence"
		return i, nil
	}
	payload, err := doJSON(ctx, b.httpClient(), http.MethodPost, endpoint(base, "/api/v1/knowledge-search"), b.headers(p), map[string]interface{}{"query": c.Knowledge.Statement, "knowledge_base_id": target})
	if err != nil {
		i.BlockingReason = "knowledge dedup/conflict search failed: " + err.Error()
		return i, nil
	}
	dup := b.DuplicateThreshold
	if dup <= 0 {
		dup = .985
	}
	conflict := b.ConflictThreshold
	if conflict <= 0 {
		conflict = .90
	}
	candidateText := normalizeText(c.Knowledge.Statement)
	for _, raw := range unwrapArray(payload) {
		item, _ := raw.(map[string]interface{})
		if item == nil {
			continue
		}
		score := floatField(item, "score", "similarity")
		content := stringField(item, "content", "text")
		resourceID := stringField(item, "knowledge_id", "id")
		if normalizeText(content) == candidateText || score >= dup {
			i.Decision = DecisionSkip
			i.Duplicate = true
			i.DuplicateResourceID = resourceID
			return i, nil
		}
		if score >= conflict && strings.TrimSpace(content) != "" {
			i.Decision = DecisionConflict
			i.Conflict = true
			i.ConflictReason = fmt.Sprintf("authoritative knowledge %s is highly similar (score %.3f); reviewer must confirm compatibility before publication", resourceID, score)
			break
		}
	}
	return i, nil
}

func (b *WeKnoraBackend) Publish(ctx context.Context, p Principal, c Candidate) (Publication, error) {
	if c.Knowledge == nil {
		return Publication{}, fmt.Errorf("knowledge candidate payload is missing")
	}
	base, err := b.baseURL(p)
	if err != nil {
		return Publication{}, err
	}
	kb := strings.TrimSpace(c.Knowledge.TargetKnowledgeBaseID)
	if !knowledgeBaseAllowed(p.KnowledgeBaseIDs, kb) {
		return Publication{}, fmt.Errorf("target knowledge base is outside the workspace scope")
	}
	payload, err := doJSON(ctx, b.httpClient(), http.MethodPost, endpoint(base, "/api/v1/knowledge-bases/"+url.PathEscape(kb)+"/knowledge/manual"), b.headers(p), map[string]interface{}{"title": c.Knowledge.Title, "content": c.Knowledge.Statement, "status": "published", "tag_ids": []string{}})
	if err != nil {
		return Publication{}, err
	}
	item := unwrapObject(payload)
	id := stringField(item, "id", "knowledge_id")
	if id == "" {
		if nested, ok := item["knowledge"].(map[string]interface{}); ok {
			id = stringField(nested, "id", "knowledge_id")
		}
	}
	if id == "" {
		return Publication{}, fmt.Errorf("WeKnora publish response did not contain a knowledge id")
	}
	return Publication{System: "weknora", ResourceID: id, URI: "weknora://knowledge-bases/" + kb + "/knowledge/" + id, Action: "create", RollbackSnapshot: &RollbackSnapshot{Action: "delete_created_knowledge", ResourceID: id}}, nil
}
func (b *WeKnoraBackend) Rollback(ctx context.Context, p Principal, c Candidate) error {
	if c.Publication == nil || c.Publication.RollbackSnapshot == nil {
		return fmt.Errorf("knowledge publication has no rollback snapshot")
	}
	id := strings.TrimSpace(c.Publication.RollbackSnapshot.ResourceID)
	if id == "" {
		return fmt.Errorf("knowledge rollback resource id is missing")
	}
	base, err := b.baseURL(p)
	if err != nil {
		return err
	}
	_, err = doJSON(ctx, b.httpClient(), http.MethodDelete, endpoint(base, "/api/v1/knowledge/"+url.PathEscape(id)), b.headers(p), nil)
	return err
}

func (b *WeKnoraBackend) verifyEvidence(ctx context.Context, base string, p Principal, evidence []EvidenceLink) (bool, error) {
	if len(evidence) == 0 {
		return false, nil
	}
	for _, ref := range evidence {
		chunk := strings.TrimSpace(ref.ChunkID)
		if chunk == "" {
			return false, fmt.Errorf("evidence %s does not contain a chunk id", strings.TrimSpace(ref.ID))
		}
		payload, err := doJSON(ctx, b.httpClient(), http.MethodGet, endpoint(base, "/api/v1/chunks/by-id/"+url.PathEscape(chunk)), b.headers(p), nil)
		if err != nil {
			return false, fmt.Errorf("verify evidence chunk %s: %w", chunk, err)
		}
		item := unwrapObject(payload)
		kb := stringField(item, "knowledge_base_id")
		if kb == "" {
			return false, fmt.Errorf("evidence chunk %s did not expose knowledge_base_id", chunk)
		}
		if !knowledgeBaseAllowed(p.KnowledgeBaseIDs, kb) {
			return false, fmt.Errorf("evidence chunk %s is outside the workspace knowledge scope", chunk)
		}
		if ref.KnowledgeBaseID != "" && strings.TrimSpace(ref.KnowledgeBaseID) != kb {
			return false, fmt.Errorf("evidence chunk %s knowledge-base mismatch", chunk)
		}
	}
	return true, nil
}
func (b *WeKnoraBackend) headers(p Principal) http.Header {
	h := make(http.Header)
	h.Set("Accept", "application/json")
	h.Set("Accept-Language", "zh-CN")
	h.Set("X-API-Key", strings.TrimSpace(p.WeKnoraAPIKey))
	h.Set("X-Tenant-ID", strings.TrimSpace(p.TenantID))
	h.Set("X-External-User-ID", strings.TrimSpace(p.UserID))
	return h
}
func (b *WeKnoraBackend) baseURL(p Principal) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(p.WeKnoraBaseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(b.DefaultBaseURL), "/")
	}
	if base == "" {
		return "", fmt.Errorf("WeKnora base URL is not configured")
	}
	return base, nil
}
func (b *WeKnoraBackend) httpClient() *http.Client {
	if b.Client == nil {
		return &http.Client{Timeout: 20 * time.Second}
	}
	return b.Client
}
func knowledgeBaseAllowed(allowed []string, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, id := range allowed {
		if strings.TrimSpace(id) == requested {
			return true
		}
	}
	return false
}
func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}
