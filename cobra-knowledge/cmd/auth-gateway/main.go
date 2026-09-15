package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
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

const sessionCookie = "leeclaw_session"

type config struct {
	listenAddr         string
	upstreamURL        *url.URL
	sandboxListenAddr  string
	sandboxUpstreamURL *url.URL
	identity           string
	password           string
	role               string
	cookieSecure       bool
	sessionTTL         time.Duration
}

type session struct {
	identity    string
	role        string
	expiresAt   time.Time
	validatedAt time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
}

type server struct {
	cfg          config
	sessions     *sessionStore
	proxy        *httputil.ReverseProxy
	sandboxProxy *httputil.ReverseProxy
	login        *template.Template
}

type loginPageData struct {
	Error string
	Next  string
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

	errCh := make(chan error, 2)
	go func() {
		log.Printf("LeeClaw OpenClaw profile gateway listening on %s", cfg.listenAddr)
		errCh <- http.ListenAndServe(cfg.listenAddr, s.routes())
	}()
	go func() {
		log.Printf("LeeClaw MCP Apps sandbox proxy listening on %s", cfg.sandboxListenAddr)
		errCh <- http.ListenAndServe(cfg.sandboxListenAddr, s.sandboxRoutes())
	}()
	log.Fatal(<-errCh)
}

func loadConfig() (config, error) {
	cfg := config{
		listenAddr:        envOr("LEECLAW_AUTH_LISTEN", ":18789"),
		sandboxListenAddr: envOr("LEECLAW_SANDBOX_LISTEN", ":18790"),
		identity:          envOr("LEECLAW_OPENCLAW_IDENTITY", ""),
		password:          envOr("OPENCLAW_GATEWAY_PASSWORD", ""),
		role:              envOr("LEECLAW_OPENCLAW_ROLE", "owner"),
		cookieSecure:      envBool("LEECLAW_AUTH_COOKIE_SECURE", false),
		sessionTTL:        12 * time.Hour,
	}

	var err error
	if cfg.upstreamURL, err = url.Parse(envOr("LEECLAW_AUTH_UPSTREAM", "http://openclaw-internal:18789")); err != nil {
		return config{}, fmt.Errorf("invalid LEECLAW_AUTH_UPSTREAM: %w", err)
	}
	if cfg.sandboxUpstreamURL, err = url.Parse(envOr("LEECLAW_SANDBOX_UPSTREAM", "http://openclaw-internal:18790")); err != nil {
		return config{}, fmt.Errorf("invalid LEECLAW_SANDBOX_UPSTREAM: %w", err)
	}
	if cfg.identity == "" {
		return config{}, errors.New("LEECLAW_OPENCLAW_IDENTITY is required")
	}
	if cfg.password == "" {
		return config{}, errors.New("OPENCLAW_GATEWAY_PASSWORD is required")
	}
	if ttlText := os.Getenv("LEECLAW_AUTH_SESSION_TTL"); ttlText != "" {
		if cfg.sessionTTL, err = time.ParseDuration(ttlText); err != nil || cfg.sessionTTL <= 0 {
			return config{}, fmt.Errorf("invalid LEECLAW_AUTH_SESSION_TTL %q", ttlText)
		}
	}
	return cfg, nil
}

func newServer(cfg config) (*server, error) {
	login, err := template.New("login").Parse(loginHTML)
	if err != nil {
		return nil, err
	}
	s := &server{
		cfg:      cfg,
		sessions: &sessionStore{sessions: make(map[string]session)},
		login:    login,
	}
	s.proxy = &httputil.ReverseProxy{
		Rewrite:       s.rewriteProxyRequest,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("OpenClaw upstream error: %v", err)
			http.Error(w, "OpenClaw 暂时不可用", http.StatusBadGateway)
		},
	}
	s.sandboxProxy = &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(cfg.sandboxUpstreamURL)
		for _, header := range []string{"Forwarded", "X-Real-Ip", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-User", "X-OpenClaw-Scopes"} {
			pr.Out.Header.Del(header)
		}
		pr.SetXForwarded()
	}, FlushInterval: -1, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("MCP Apps sandbox upstream error: %v", err)
		http.Error(w, "MCP Apps sandbox proxy unavailable", http.StatusBadGateway)
	}}
	return s, nil
}

func (s *server) sandboxRoutes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.sandboxProxy.ServeHTTP(w, r) })
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET /login", s.showLogin)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("/", s.requireSession)
	return securityHeaders(mux)
}

func (s *server) showLogin(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.currentSession(r); ok {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.renderLogin(w, loginPageData{Next: safeNext(r.URL.Query().Get("next"))})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, loginPageData{Error: "登录请求格式不正确", Next: "/"})
		return
	}
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if password == "" {
		s.renderLogin(w, loginPageData{Error: "请输入 OpenClaw 密码", Next: next})
		return
	}

	auth, err := s.authenticate(password)
	if err != nil {
		log.Printf("OpenClaw profile login rejected: %v", err)
		s.renderLogin(w, loginPageData{Error: "OpenClaw 密码不正确", Next: next})
		return
	}

	id, err := randomID()
	if err != nil {
		http.Error(w, "无法创建登录会话", http.StatusInternalServerError)
		return
	}
	s.sessions.put(id, session{
		identity:    auth.identity,
		role:        auth.role,
		expiresAt:   time.Now().Add(s.cfg.sessionTTL),
		validatedAt: time.Now(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.cfg.sessionTTL.Seconds()),
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.delete(cookie.Value)
	}
	clearSessionCookie(w, s.cfg.cookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) requireSession(w http.ResponseWriter, r *http.Request) {
	id, sess, ok := s.currentSession(r)
	if !ok {
		s.unauthorized(w, r)
		return
	}
	if time.Since(sess.validatedAt) > 30*time.Second {
		updated, err := s.validateSession(sess)
		if err != nil {
			s.sessions.delete(id)
			clearSessionCookie(w, s.cfg.cookieSecure)
			s.unauthorized(w, r)
			return
		}
		sess = updated
		s.sessions.put(id, sess)
	}
	r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sess))
	s.proxy.ServeHTTP(w, r)
}

func (s *server) unauthorized(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && acceptsHTML(r) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(safeNext(r.URL.RequestURI())), http.StatusSeeOther)
		return
	}
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

func (s *server) rewriteProxyRequest(pr *httputil.ProxyRequest) {
	sess, _ := pr.In.Context().Value(sessionContextKey{}).(session)
	originalHost := pr.In.Host
	pr.SetURL(s.cfg.upstreamURL)
	for _, header := range []string{
		"Forwarded", "X-Real-Ip", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto",
		"X-Forwarded-User", "X-Forwarded-Email", "X-Openclaw-Scopes",
		"X-OpenClaw-Scopes", "X-Remote-User", "Remote-User",
	} {
		pr.Out.Header.Del(header)
	}
	pr.SetXForwarded()
	pr.Out.Header.Set("X-Forwarded-User", sess.identity)
	pr.Out.Header.Set("X-Forwarded-Host", originalHost)
	if pr.In.TLS != nil {
		pr.Out.Header.Set("X-Forwarded-Proto", "https")
	} else {
		pr.Out.Header.Set("X-Forwarded-Proto", "http")
	}
	pr.Out.Header.Set("X-OpenClaw-Scopes", scopesForRole(sess.role))
}

type authenticatedUser struct {
	identity string
	role     string
}

func (s *server) authenticate(password string) (authenticatedUser, error) {
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.password)) != 1 {
		return authenticatedUser{}, errors.New("invalid credentials")
	}
	return authenticatedUser{identity: s.cfg.identity, role: s.cfg.role}, nil
}

func (s *server) validateSession(sess session) (session, error) {
	if time.Now().After(sess.expiresAt) {
		return session{}, errors.New("session expired")
	}
	if sess.identity != s.cfg.identity {
		return session{}, errors.New("OpenClaw identity binding changed")
	}
	sess.validatedAt = time.Now()
	return sess, nil
}

func scopesForRole(role string) string {
	switch strings.ToLower(role) {
	case "owner", "admin":
		return "operator.admin,operator.read,operator.write,operator.approvals,operator.questions,operator.pairing,operator.talk.secrets"
	case "contributor", "editor":
		return "operator.read,operator.write,operator.approvals,operator.questions"
	default:
		return "operator.read"
	}
}

func (s *server) currentSession(r *http.Request) (string, session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "", session{}, false
	}
	sess, ok := s.sessions.get(cookie.Value)
	if !ok || time.Now().After(sess.expiresAt) {
		if ok {
			s.sessions.delete(cookie.Value)
		}
		return "", session{}, false
	}
	return cookie.Value, sess, true
}

func (s *server) renderLogin(w http.ResponseWriter, data loginPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := s.login.Execute(w, data); err != nil {
		log.Printf("render login page: %v", err)
	}
}

func (s *sessionStore) get(id string) (session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *sessionStore) put(id string, sess session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") || r.Header.Get("Accept") == ""
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

type sessionContextKey struct{}

const loginHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>登录 LeeClaw</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #090b10; color: #f3f4f6; }
    main { width: min(400px, calc(100vw - 32px)); padding: 32px; border: 1px solid #272b35; border-radius: 16px; background: #11141b; box-shadow: 0 24px 80px #0008; }
    h1 { margin: 0 0 8px; font-size: 25px; }
    p { margin: 0 0 26px; color: #9ca3af; font-size: 14px; line-height: 1.5; }
    label { display: block; margin: 16px 0 7px; color: #d1d5db; font-size: 13px; }
    input { width: 100%; padding: 12px 13px; border: 1px solid #343947; border-radius: 9px; background: #0b0e14; color: #fff; font: inherit; outline: none; }
    input:focus { border-color: #7c8cff; box-shadow: 0 0 0 3px #596cff25; }
    button { width: 100%; margin-top: 24px; padding: 12px; border: 0; border-radius: 9px; background: #6576f7; color: #fff; font: inherit; font-weight: 650; cursor: pointer; }
    button:hover { background: #7484ff; }
    .error { margin: 0 0 12px; padding: 10px 12px; border-radius: 8px; background: #7f1d1d55; color: #fecaca; }
    small { display: block; margin-top: 18px; color: #6b7280; text-align: center; }
  </style>
</head>
<body><main>
  <h1>LeeClaw</h1>
  <p>使用 OpenClaw / LeeClaw 密码登录。Workspace、OpenViking 与 WeKnora 范围由服务端根据 OpenClaw Profile 绑定。</p>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  <form method="post" action="/auth/login">
    <input type="hidden" name="next" value="{{.Next}}">
    <label for="password">密码</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required autofocus>
    <button type="submit">登录</button>
  </form>
  <small>OpenClaw 是唯一人类身份；后端凭据不会发送到浏览器</small>
</main></body></html>`
