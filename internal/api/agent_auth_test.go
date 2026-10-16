package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/controller"
	appcore "aeswibon.com/github/gitopsctl/internal/core/app"
	clustercore "aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/events"
	"aeswibon.com/github/gitopsctl/internal/session"
	"aeswibon.com/github/gitopsctl/internal/session/sessiontest"
	"go.uber.org/zap"
)

const agentConfig = `
policy:
  allowed_kinds: [ConfigMap]
  allowed_namespaces: ["agent-*"]
agents:
  - id: alpha
    cluster: dev
    repos: ["https://git.example/"]
    max_sessions: 3
  - id: beta
    cluster: dev
    repos: ["https://git.example/"]
    max_sessions: 3
`

func newAgentServer(t *testing.T) (*Server, *auditRecorder) {
	t.Helper()
	cfg, err := session.ParseConfig([]byte(agentConfig))
	if err != nil {
		t.Fatal(err)
	}
	cluster := sessiontest.NewCluster()
	mgr, err := session.NewManager(zap.NewNop(), cfg, func(string) (session.Cluster, error) { return cluster, nil }, filepath.Join(t.TempDir(), "s.json"),
		session.WithFetcher(func(context.Context, string, string) (string, string, func(), error) {
			return t.TempDir(), "c", func() {}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	rec := &auditRecorder{}
	ctrl := controller.NewController(zap.NewNop(), appcore.NewApplications(), clustercore.NewClusters(), controller.WithEmitter(rec))
	t.Cleanup(ctrl.Stop)
	srv := NewServer(zap.NewNop(), appcore.NewApplications(), clustercore.NewClusters(), ctrl, nil, nil,
		WithAPITokens(map[string]Scope{"admin-tok": ScopeAdmin, "sync-tok": ScopeSync, "read-tok": ScopeRead}),
		WithAgentTokens(map[string]string{"alpha-tok": "alpha", "beta-tok": "beta"}),
		WithSessions(mgr))
	return srv, rec
}

func doJSON(s *Server, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Echo().ServeHTTP(rec, req)
	return rec
}

func TestAgentTokens_ConfinedToSessions(t *testing.T) {
	srv, _ := newAgentServer(t)
	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   string
		want   int
	}{
		{"agent creates its own session", http.MethodPost, "/api/v1/sessions", "alpha-tok", `{}`, http.StatusCreated},
		{"agent cannot read applications", http.MethodGet, "/api/v1/applications", "alpha-tok", "", http.StatusForbidden},
		{"agent cannot register applications", http.MethodPost, "/api/v1/applications", "alpha-tok", `{}`, http.StatusForbidden},
		{"agent cannot touch clusters", http.MethodDelete, "/api/v1/clusters/x", "alpha-tok", "", http.StatusForbidden},
		{"agent cannot sync", http.MethodPost, "/api/v1/applications/x/sync", "alpha-tok", "", http.StatusForbidden},
		{"agent health stays open", http.MethodGet, "/health", "alpha-tok", "", http.StatusOK},
		{"read token cannot use sessions", http.MethodGet, "/api/v1/sessions", "read-tok", "", http.StatusForbidden},
		{"sync token cannot use sessions", http.MethodPost, "/api/v1/sessions", "sync-tok", `{"agent":"alpha"}`, http.StatusForbidden},
		{"admin must name an agent", http.MethodPost, "/api/v1/sessions", "admin-tok", `{}`, http.StatusBadRequest},
		{"admin can create for an agent", http.MethodPost, "/api/v1/sessions", "admin-tok", `{"agent":"beta"}`, http.StatusCreated},
		{"unknown token", http.MethodPost, "/api/v1/sessions", "nope", `{}`, http.StatusUnauthorized},
		{"no token", http.MethodGet, "/api/v1/sessions", "", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := doJSON(srv, tc.method, tc.path, tc.token, tc.body); rec.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestAgentTokens_CannotSeeOtherAgentsSessions(t *testing.T) {
	srv, _ := newAgentServer(t)
	rec := doJSON(srv, http.MethodPost, "/api/v1/sessions", "alpha-tok", `{}`)
	var created session.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Agent != "alpha" {
		t.Fatalf("session must belong to the token's agent, got %q", created.Agent)
	}

	if got := doJSON(srv, http.MethodGet, "/api/v1/sessions/"+created.ID, "beta-tok", "").Code; got != http.StatusNotFound {
		t.Fatalf("other agent: %d", got)
	}
	if got := doJSON(srv, http.MethodDelete, "/api/v1/sessions/"+created.ID, "beta-tok", "").Code; got != http.StatusNotFound {
		t.Fatalf("other agent delete: %d", got)
	}
	if got := doJSON(srv, http.MethodGet, "/api/v1/sessions/"+created.ID, "alpha-tok", "").Code; got != http.StatusOK {
		t.Fatalf("owner: %d", got)
	}
	if got := doJSON(srv, http.MethodGet, "/api/v1/sessions/"+created.ID, "admin-tok", "").Code; got != http.StatusOK {
		t.Fatalf("admin: %d", got)
	}
	if got := doJSON(srv, http.MethodPost, "/api/v1/sessions", "alpha-tok", `{"agent":"beta"}`).Code; got != http.StatusForbidden {
		t.Fatalf("impersonation: %d", got)
	}
}

func TestAgentCalls_AreAuditedWithAgentIdentity(t *testing.T) {
	srv, rec := newAgentServer(t)
	doJSON(srv, http.MethodPost, "/api/v1/sessions", "alpha-tok", `{}`)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.data) != 1 || rec.typs[0] != events.TypeAPIAudit {
		t.Fatalf("expected one audit event, got %v", rec.typs)
	}
	if rec.data[0]["agent"] != "alpha" || rec.data[0]["path"] != "/api/v1/sessions" {
		t.Fatalf("unexpected audit payload %v", rec.data[0])
	}
	for _, v := range rec.data[0] {
		if s, ok := v.(string); ok && strings.Contains(s, "alpha-tok") {
			t.Fatal("the token must never be audited")
		}
	}
}

func TestParseAgentTokens(t *testing.T) {
	input := "# c\nadmin a-tok\nagent ag-tok alpha\nagent other-tok beta\n"
	agents, err := ParseAgentTokens(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents["ag-tok"] != "alpha" || agents["other-tok"] != "beta" {
		t.Fatalf("unexpected agents %v", agents)
	}
	tokens, err := ParseTokens(strings.NewReader(input))
	if err != nil || len(tokens) != 1 || tokens["a-tok"] != ScopeAdmin {
		t.Fatalf("scope parsing must ignore agent lines: %v %v", tokens, err)
	}
	for name, bad := range map[string]string{
		"agent without id":  "agent tok\n",
		"agent extra field": "agent tok alpha extra\n",
		"shared token":      "admin same\nagent same alpha\n",
	} {
		if _, err := ParseAgentTokens(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSessionsAPIAbsentWithoutManager(t *testing.T) {
	srv, _ := newAuthServer(t, map[string]Scope{"admin-tok": ScopeAdmin})
	if got := doJSON(srv, http.MethodGet, "/api/v1/sessions", "admin-tok", "").Code; got != http.StatusNotFound {
		t.Fatalf("got %d", got)
	}
}
