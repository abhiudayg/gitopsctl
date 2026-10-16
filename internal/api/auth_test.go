package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/controller"
	appcore "aeswibon.com/github/gitopsctl/internal/core/app"
	clustercore "aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/events"
	"go.uber.org/zap"
)

type auditRecorder struct {
	mu   sync.Mutex
	typs []events.Type
	data []map[string]any
}

func (r *auditRecorder) Emit(_ context.Context, typ events.Type, data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.typs = append(r.typs, typ)
	r.data = append(r.data, data)
}

func newAuthServer(t *testing.T, tokens map[string]Scope, opts ...ServerOption) (*Server, *auditRecorder) {
	t.Helper()
	rec := &auditRecorder{}
	ctrl := controller.NewController(zap.NewNop(), appcore.NewApplications(), clustercore.NewClusters(), controller.WithEmitter(rec))
	t.Cleanup(ctrl.Stop)
	opts = append(opts, WithAPITokens(tokens))
	return NewServer(zap.NewNop(), appcore.NewApplications(), clustercore.NewClusters(), ctrl, nil, nil, opts...), rec
}

func doRequest(s *Server, method, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	s.Echo().ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddleware(t *testing.T) {
	tokens := map[string]Scope{"read-tok": ScopeRead, "sync-tok": ScopeSync, "admin-tok": ScopeAdmin}
	tests := []struct {
		name   string
		method string
		path   string
		auth   string
		want   int
	}{
		{"missing token", http.MethodGet, "/api/v1/applications", "", http.StatusUnauthorized},
		{"wrong token", http.MethodGet, "/api/v1/applications", "Bearer nope", http.StatusUnauthorized},
		{"non-bearer scheme", http.MethodGet, "/api/v1/applications", "Basic read-tok", http.StatusUnauthorized},
		{"read token can read", http.MethodGet, "/api/v1/applications", "Bearer read-tok", http.StatusOK},
		{"read token cannot sync", http.MethodPost, "/api/v1/applications/x/sync", "Bearer read-tok", http.StatusForbidden},
		{"sync token can sync", http.MethodPost, "/api/v1/applications/x/sync", "Bearer sync-tok", http.StatusNotFound},
		{"sync token cannot delete", http.MethodDelete, "/api/v1/applications/x", "Bearer sync-tok", http.StatusForbidden},
		{"admin token can delete", http.MethodDelete, "/api/v1/applications/x", "Bearer admin-tok", http.StatusNotFound},
		{"health stays open", http.MethodGet, "/health", "", http.StatusOK},
		{"metrics stays open", http.MethodGet, "/metrics", "", http.StatusOK},
	}
	srv, _ := newAuthServer(t, tokens)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(srv, tc.method, tc.path, tc.auth)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (body %q)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("missing WWW-Authenticate challenge")
			}
		})
	}
}

func TestAuthDisabledWithoutTokens(t *testing.T) {
	srv, _ := newAuthServer(t, nil)
	if rec := doRequest(srv, http.MethodGet, "/api/v1/applications", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected open API without tokens, got %d", rec.Code)
	}
}

func TestAuthAuditsMutatingCallsWithoutLeakingToken(t *testing.T) {
	srv, rec := newAuthServer(t, map[string]Scope{"admin-tok": ScopeAdmin})

	doRequest(srv, http.MethodGet, "/api/v1/applications", "Bearer admin-tok")
	doRequest(srv, http.MethodDelete, "/api/v1/applications/x", "Bearer admin-tok")
	doRequest(srv, http.MethodDelete, "/api/v1/applications/x", "Bearer wrong")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.data) != 1 || rec.typs[0] != events.TypeAPIAudit {
		t.Fatalf("expected exactly one audit event, got %v", rec.typs)
	}
	got := rec.data[0]
	if got["method"] != http.MethodDelete || got["path"] != "/api/v1/applications/x" || got["status"] != http.StatusNotFound {
		t.Fatalf("unexpected audit payload: %v", got)
	}
	if id, _ := got["token_id"].(string); id == "" || strings.Contains(id, "admin-tok") {
		t.Fatalf("token_id must be a non-secret identifier, got %q", id)
	}
}

func TestCORSDisabledByDefaultAndConfigurable(t *testing.T) {
	preflight := func(s *Server) string {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/applications", nil)
		req.Header.Set("Origin", "https://ui.example")
		req.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		s.Echo().ServeHTTP(rec, req)
		return rec.Header().Get("Access-Control-Allow-Origin")
	}

	closed, _ := newAuthServer(t, nil)
	if got := preflight(closed); got != "" {
		t.Fatalf("CORS should be off by default, got %q", got)
	}
	open, _ := newAuthServer(t, nil, WithCORSOrigins([]string{"https://ui.example"}))
	if got := preflight(open); got != "https://ui.example" {
		t.Fatalf("expected configured origin to be allowed, got %q", got)
	}
}

func TestParseTokens(t *testing.T) {
	tokens, err := ParseTokens(strings.NewReader("# comment\n\nadmin a-tok\nREAD r-tok\n"))
	if err != nil {
		t.Fatal(err)
	}
	if tokens["a-tok"] != ScopeAdmin || tokens["r-tok"] != ScopeRead || len(tokens) != 2 {
		t.Fatalf("unexpected tokens: %v", tokens)
	}

	for name, input := range map[string]string{
		"unknown scope": "root tok\n",
		"missing token": "admin\n",
		"extra fields":  "admin a b\n",
	} {
		if _, err := ParseTokens(strings.NewReader(input)); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRequiredScope(t *testing.T) {
	tests := []struct {
		method, path string
		want         Scope
	}{
		{http.MethodGet, "/api/v1/events", ScopeRead},
		{http.MethodHead, "/api/v1/applications", ScopeRead},
		{http.MethodPost, "/api/v1/clusters/c/check", ScopeSync},
		{http.MethodPost, "/api/v1/applications/a/approve", ScopeSync},
		{http.MethodPost, "/api/v1/applications", ScopeAdmin},
		{http.MethodDelete, "/api/v1/clusters/c", ScopeAdmin},
	}
	for _, tc := range tests {
		if got := requiredScope(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
}
