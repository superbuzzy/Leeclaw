package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOpenClawUsesNativeAuthenticationSurface(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "native-openclaw") }))
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, upstream.URL, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	gateway.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "native-openclaw" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestKnowledgeTicketExchangeAndCredentialInjection(t *testing.T) {
	var received http.Header
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer api.Close()
	ui := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><head></head><body>WeKnora</body></html>")
	}))
	defer ui.Close()
	gateway := newTestGateway(t, ui.URL, ui.URL, api.URL)
	ticket := signTestTicket(t, "profile-owner", "leeclaw-local", "test-secret")
	exchange := httptest.NewRequest(http.MethodGet, "/auth/session?ticket="+url.QueryEscape(ticket)+"&next="+url.QueryEscape("/platform/knowledge-bases?leeclaw_embed=1"), nil)
	exchangeResponse := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(exchangeResponse, exchange)
	if exchangeResponse.Code != http.StatusSeeOther {
		t.Fatalf("exchange status=%d body=%s", exchangeResponse.Code, exchangeResponse.Body.String())
	}
	cookies := exchangeResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != knowledgeCookie || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	request.Header.Set("Authorization", "Bearer attacker")
	request.Header.Set("X-API-Key", "attacker")
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("api status=%d body=%s", response.Code, response.Body.String())
	}
	if received.Get("X-API-Key") != "wk-secret" || received.Get("X-Tenant-ID") != "10000" || received.Get("X-External-User-ID") != "profile-owner" {
		t.Fatalf("headers=%v", received)
	}
}

func TestKnowledgeTicketCreatesServerSideWeKnoraUserSession(t *testing.T) {
	var received http.Header
	weknora := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"user-session-token","active_tenant":{"id":10000}}`)
			return
		}
		received = r.Header.Clone()
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer weknora.Close()
	gateway := newTestGateway(t, weknora.URL, weknora.URL, weknora.URL)
	gateway.cfg.weknoraEmail = "mapped@local.invalid"
	gateway.cfg.weknoraPassword = "server-only"
	cookie := establishTestSession(t, gateway)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/user/favorites", nil)
	request.Header.Set("Authorization", "Bearer browser-value")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if received.Get("Authorization") != "Bearer user-session-token" || received.Get("X-API-Key") != "" {
		t.Fatalf("headers=%v", received)
	}
}

func TestKnowledgeUIEmbedInjection(t *testing.T) {
	ui := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><head></head><body>WeKnora</body></html>")
	}))
	defer ui.Close()
	gateway := newTestGateway(t, ui.URL, ui.URL, ui.URL)
	cookie := establishTestSession(t, gateway)
	request := httptest.NewRequest(http.MethodGet, "/platform/knowledge-bases?leeclaw_embed=1", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "aside_box") || !strings.Contains(response.Body.String(), "WeKnora") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInvalidKnowledgeTicketIsRejected(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, upstream.URL, upstream.URL)
	response := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auth/session?ticket=invalid", nil))
	if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
		t.Fatalf("status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}

func TestSandboxProxyStripsIdentity(t *testing.T) {
	var received http.Header
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sandbox.Close()
	gateway := newTestGateway(t, sandbox.URL, sandbox.URL, sandbox.URL)
	request := httptest.NewRequest(http.MethodGet, "/app/ticket", nil)
	request.Header.Set("X-Forwarded-User", "attacker")
	response := httptest.NewRecorder()
	gateway.sandboxRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || received.Get("X-Forwarded-User") != "" {
		t.Fatalf("status=%d headers=%v", response.Code, received)
	}
}

func signTestTicket(t *testing.T, profileID, workspaceID, secret string) string {
	t.Helper()
	raw, err := json.Marshal(ticketClaims{ProfileID: profileID, WorkspaceID: workspaceID, Exp: time.Now().Add(time.Minute).Unix(), Nonce: "nonce"})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func establishTestSession(t *testing.T, gateway *server) *http.Cookie {
	t.Helper()
	ticket := signTestTicket(t, "profile-owner", "leeclaw-local", "test-secret")
	response := httptest.NewRecorder()
	gateway.knowledgeRoutes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auth/session?ticket="+url.QueryEscape(ticket), nil))
	return response.Result().Cookies()[0]
}

func newTestGateway(t *testing.T, openclaw, ui, api string) *server {
	t.Helper()
	parse := func(raw string) *url.URL {
		value, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	s, err := newServer(config{listenAddr: ":0", sandboxListenAddr: ":0", knowledgeListenAddr: ":0", upstreamURL: parse(openclaw), sandboxUpstreamURL: parse(openclaw), knowledgeUIURL: parse(ui), knowledgeAPIURL: parse(api), knowledgeSecret: "test-secret", weknoraAPIKey: "wk-secret", weknoraTenantID: "10000", sessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
