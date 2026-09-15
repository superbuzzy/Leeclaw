package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginAndProxyOpenClawProfileIdentity(t *testing.T) {
	var received http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		_, _ = io.WriteString(w, "openclaw")
	}))
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, upstream.URL)
	form := url.Values{"password": {"test-password"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	gateway.routes().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "localhost:18789"
	request.Header.Set("X-Forwarded-User", "attacker")
	request.Header.Set("X-OpenClaw-Scopes", "operator.admin")
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	gateway.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "openclaw" {
		t.Fatalf("proxy status=%d body=%q", response.Code, response.Body.String())
	}
	if got := received.Get("X-Forwarded-User"); got != "profile-owner" {
		t.Fatalf("identity=%q", got)
	}
	if got := received.Get("X-OpenClaw-Scopes"); !strings.Contains(got, "operator.admin") {
		t.Fatalf("scopes=%q", got)
	}
}

func TestInvalidOpenClawPasswordDoesNotCreateSession(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, upstream.URL)
	form := url.Values{"password": {"wrong"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	gateway.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 0 {
		t.Fatalf("status=%d cookies=%#v", response.Code, response.Result().Cookies())
	}
}

func TestSandboxProxyIsPublicAndStripsIdentity(t *testing.T) {
	var received http.Header
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sandbox.Close()
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, sandbox.URL)
	request := httptest.NewRequest(http.MethodGet, "/app/ticket", nil)
	request.Header.Set("X-Forwarded-User", "attacker")
	response := httptest.NewRecorder()
	gateway.sandboxRoutes().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("sandbox status=%d", response.Code)
	}
	if received.Get("X-Forwarded-User") != "" {
		t.Fatalf("identity leaked to sandbox: %q", received.Get("X-Forwarded-User"))
	}
}

func TestUnauthenticatedHTMLRedirectsToLogin(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	gateway := newTestGateway(t, upstream.URL, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, "/knowledge?tab=all", nil)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	gateway.routes().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/login?next=") {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func newTestGateway(t *testing.T, upstream, sandbox string) *server {
	t.Helper()
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	sandboxURL, err := url.Parse(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newServer(config{listenAddr: ":0", sandboxListenAddr: ":0", upstreamURL: upstreamURL, sandboxUpstreamURL: sandboxURL, identity: "profile-owner", password: "test-password", role: "owner", sessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
