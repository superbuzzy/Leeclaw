package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const knowledgeCookie = "leeclaw_knowledge_session"

type config struct {
	listenAddr, sandboxListenAddr, knowledgeListenAddr               string
	upstreamURL, sandboxUpstreamURL, knowledgeUIURL, knowledgeAPIURL *url.URL
	knowledgeSecret, weknoraAPIKey, weknoraTenantID                  string
	weknoraEmail, weknoraPassword                                    string
	cookieSecure                                                     bool
	sessionTTL                                                       time.Duration
}

type session struct {
	identity, workspaceID, weknoraToken string
	expiresAt                           time.Time
}
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
}
type ticketClaims struct {
	ProfileID   string `json:"profileId"`
	WorkspaceID string `json:"workspaceId"`
	Exp         int64  `json:"exp"`
	Nonce       string `json:"nonce"`
}
type sessionContextKey struct{}

type server struct {
	cfg                                 config
	sessions                            *sessionStore
	proxy, sandboxProxy, knowledgeProxy *httputil.ReverseProxy
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	s, err := newServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	errCh := make(chan error, 3)
	go func() {
		log.Printf("OpenClaw gateway proxy listening on %s", cfg.listenAddr)
		errCh <- http.ListenAndServe(cfg.listenAddr, s.routes())
	}()
	go func() {
		log.Printf("MCP Apps sandbox proxy listening on %s", cfg.sandboxListenAddr)
		errCh <- http.ListenAndServe(cfg.sandboxListenAddr, s.sandboxRoutes())
	}()
	go func() {
		log.Printf("Knowledge UI proxy listening on %s", cfg.knowledgeListenAddr)
		errCh <- http.ListenAndServe(cfg.knowledgeListenAddr, s.knowledgeRoutes())
	}()
	log.Fatal(<-errCh)
}

func loadConfig() (config, error) {
	c := config{
		listenAddr:          envOr("LEECLAW_AUTH_LISTEN", ":18789"),
		sandboxListenAddr:   envOr("LEECLAW_SANDBOX_LISTEN", ":18790"),
		knowledgeListenAddr: envOr("LEECLAW_KNOWLEDGE_LISTEN", ":18791"),
		knowledgeSecret:     strings.TrimSpace(os.Getenv("LEECLAW_RUNTIME_TOKEN")),
		weknoraAPIKey:       strings.TrimSpace(os.Getenv("WEKNORA_API_KEY_LEECLAW")),
		weknoraTenantID:     envOr("LEECLAW_WEKNORA_TENANT_ID", "10000"),
		weknoraEmail:        strings.TrimSpace(os.Getenv("WEKNORA_BOOTSTRAP_EMAIL")),
		weknoraPassword:     strings.TrimSpace(os.Getenv("WEKNORA_BOOTSTRAP_PASSWORD")),
		cookieSecure:        envBool("LEECLAW_AUTH_COOKIE_SECURE", false), sessionTTL: 12 * time.Hour,
	}
	var err error
	for target, raw := range map[**url.URL]string{
		&c.upstreamURL:        envOr("LEECLAW_AUTH_UPSTREAM", "http://openclaw:18789"),
		&c.sandboxUpstreamURL: envOr("LEECLAW_SANDBOX_UPSTREAM", "http://openclaw:18790"),
		&c.knowledgeUIURL:     envOr("LEECLAW_KNOWLEDGE_UI_UPSTREAM", "http://weknora-ui:80"),
		&c.knowledgeAPIURL:    envOr("LEECLAW_KNOWLEDGE_API_UPSTREAM", "http://weknora:8080"),
	} {
		*target, err = url.Parse(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid upstream %q: %w", raw, err)
		}
	}
	if c.knowledgeSecret == "" || c.weknoraAPIKey == "" {
		return config{}, errors.New("LEECLAW_RUNTIME_TOKEN and WEKNORA_API_KEY_LEECLAW are required")
	}
	if raw := os.Getenv("LEECLAW_AUTH_SESSION_TTL"); raw != "" {
		c.sessionTTL, err = time.ParseDuration(raw)
		if err != nil || c.sessionTTL <= 0 {
			return config{}, fmt.Errorf("invalid session TTL %q", raw)
		}
	}
	return c, nil
}

func newServer(c config) (*server, error) {
	s := &server{cfg: c, sessions: &sessionStore{sessions: map[string]session{}}}
	s.proxy = reverseProxy(c.upstreamURL, "OpenClaw", func(pr *httputil.ProxyRequest) { stripIdentity(pr.Out.Header); pr.SetXForwarded() }, nil)
	s.sandboxProxy = reverseProxy(c.sandboxUpstreamURL, "MCP Apps sandbox", func(pr *httputil.ProxyRequest) { stripIdentity(pr.Out.Header); pr.SetXForwarded() }, nil)
	s.knowledgeProxy = &httputil.ReverseProxy{
		Rewrite:        s.rewriteKnowledge,
		ModifyResponse: injectKnowledgeEmbedMode,
		FlushInterval:  -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("Knowledge upstream error: %v", err)
			http.Error(w, "Knowledge unavailable", http.StatusBadGateway)
		},
	}
	return s, nil
}

func reverseProxy(target *url.URL, name string, rewrite func(*httputil.ProxyRequest), modify func(*http.Response) error) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { pr.SetURL(target); rewrite(pr) }, ModifyResponse: modify, FlushInterval: -1, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("%s upstream error: %v", name, err)
		http.Error(w, name+" unavailable", http.StatusBadGateway)
	}}
}

func (s *server) routes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.proxy.ServeHTTP(w, r) })
}
func (s *server) sandboxRoutes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.sandboxProxy.ServeHTTP(w, r) })
}
func (s *server) knowledgeRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET /auth/session", s.exchangeKnowledgeTicket)
	mux.HandleFunc("/", s.requireKnowledgeSession)
	return knowledgeSecurityHeaders(mux)
}

func (s *server) exchangeKnowledgeTicket(w http.ResponseWriter, r *http.Request) {
	claims, err := verifyTicket(r.URL.Query().Get("ticket"), s.cfg.knowledgeSecret)
	if err != nil {
		http.Error(w, "invalid or expired Knowledge session", http.StatusUnauthorized)
		return
	}
	id, err := randomID()
	if err != nil {
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}
	weknoraToken, err := s.createWeKnoraSession(r.Context())
	if err != nil {
		log.Printf("Knowledge session mapping failed for profile %q: %v", claims.ProfileID, err)
		http.Error(w, "Knowledge identity mapping unavailable", http.StatusBadGateway)
		return
	}
	expires := time.Now().Add(s.cfg.sessionTTL)
	s.sessions.put(id, session{identity: claims.ProfileID, workspaceID: claims.WorkspaceID, weknoraToken: weknoraToken, expiresAt: expires})
	http.SetCookie(w, &http.Cookie{Name: knowledgeCookie, Value: id, Path: "/", HttpOnly: true, Secure: s.cfg.cookieSecure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(s.cfg.sessionTTL.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, safeKnowledgeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

func (s *server) requireKnowledgeSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(knowledgeCookie)
	if err != nil {
		http.Error(w, "Knowledge authentication required", http.StatusUnauthorized)
		return
	}
	sess, ok := s.sessions.get(cookie.Value)
	if !ok || time.Now().After(sess.expiresAt) {
		s.sessions.delete(cookie.Value)
		http.Error(w, "Knowledge session expired", http.StatusUnauthorized)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sess))
	s.knowledgeProxy.ServeHTTP(w, r)
}

func (s *server) rewriteKnowledge(pr *httputil.ProxyRequest) {
	sess, _ := pr.In.Context().Value(sessionContextKey{}).(session)
	api := strings.HasPrefix(pr.In.URL.Path, "/api/") || strings.HasPrefix(pr.In.URL.Path, "/files") || strings.HasPrefix(pr.In.URL.Path, "/r/")
	if api {
		pr.SetURL(s.cfg.knowledgeAPIURL)
	} else {
		pr.SetURL(s.cfg.knowledgeUIURL)
		pr.Out.Header.Del("Accept-Encoding")
	}
	stripIdentity(pr.Out.Header)
	for _, h := range []string{"Authorization", "X-API-Key", "X-Tenant-ID", "X-External-User-ID", "X-External-User-Token"} {
		pr.Out.Header.Del(h)
	}
	if api {
		if sess.weknoraToken != "" {
			pr.Out.Header.Set("Authorization", "Bearer "+sess.weknoraToken)
		} else {
			pr.Out.Header.Set("X-API-Key", s.cfg.weknoraAPIKey)
		}
		pr.Out.Header.Set("X-Tenant-ID", s.cfg.weknoraTenantID)
		pr.Out.Header.Set("X-External-User-ID", sess.identity)
	}
	pr.SetXForwarded()
}

func (s *server) createWeKnoraSession(ctx context.Context) (string, error) {
	if s.cfg.weknoraEmail == "" || s.cfg.weknoraPassword == "" {
		return "", nil
	}
	payload, err := json.Marshal(map[string]string{"email": s.cfg.weknoraEmail, "password": s.cfg.weknoraPassword})
	if err != nil {
		return "", err
	}
	loginURL := *s.cfg.knowledgeAPIURL
	loginURL.Path = strings.TrimRight(loginURL.Path, "/") + "/api/v1/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL.String(), strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("WeKnora login returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Token        string `json:"token"`
		ActiveTenant struct {
			ID json.Number `json:"id"`
		} `json:"active_tenant"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", errors.New("WeKnora login response has no token")
	}
	if tenant := result.ActiveTenant.ID.String(); tenant != "" && tenant != s.cfg.weknoraTenantID {
		return "", fmt.Errorf("WeKnora tenant mismatch: expected %s, got %s", s.cfg.weknoraTenantID, tenant)
	}
	return result.Token, nil
}

func injectKnowledgeEmbedMode(resp *http.Response) error {
	// WeKnora may protect its standalone UI with SAMEORIGIN. LeeClaw deliberately
	// serves the authenticated native page from :18791 inside OpenClaw on :18789.
	resp.Header.Del("X-Frame-Options")
	resp.Header.Del("Content-Security-Policy")
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	injection := `<style>html.leeclaw-embed .aside_box{display:none!important}html.leeclaw-embed .platform-route-outlet{width:100%!important}html.leeclaw-embed .main{min-width:0!important}</style><script>(function(){if(new URLSearchParams(location.search).has('leeclaw_embed'))document.documentElement.classList.add('leeclaw-embed');localStorage.setItem('weknora_token','leeclaw-workspace-session');localStorage.setItem('weknora_lite_mode','false')})()</script>`
	body = []byte(strings.Replace(string(body), "</head>", injection+"</head>", 1))
	resp.Body = io.NopCloser(strings.NewReader(string(body)))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("Content-Encoding")
	return nil
}

func verifyTicket(ticket, secret string) (ticketClaims, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 {
		return ticketClaims{}, errors.New("malformed ticket")
	}
	want := hmac.New(sha256.New, []byte(secret))
	_, _ = want.Write([]byte(parts[0]))
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want.Sum(nil)) {
		return ticketClaims{}, errors.New("invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ticketClaims{}, err
	}
	var claims ticketClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ticketClaims{}, err
	}
	if claims.ProfileID == "" || claims.WorkspaceID == "" || claims.Nonce == "" || claims.Exp < time.Now().Unix() || claims.Exp > time.Now().Add(2*time.Minute).Unix() {
		return ticketClaims{}, errors.New("invalid claims")
	}
	return claims, nil
}

func stripIdentity(h http.Header) {
	for _, k := range []string{"Forwarded", "X-Real-Ip", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-User", "X-OpenClaw-Scopes", "X-Remote-User", "Remote-User"} {
		h.Del(k)
	}
}
func safeKnowledgeNext(next string) string {
	if !strings.HasPrefix(next, "/platform/knowledge-bases") {
		return "/platform/knowledge-bases?leeclaw_embed=1"
	}
	return next
}
func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *sessionStore) get(id string) (session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.sessions[id]
	return value, ok
}
func (s *sessionStore) put(id string, value session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = value
}
func (s *sessionStore) delete(id string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.sessions, id) }
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
func knowledgeSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
