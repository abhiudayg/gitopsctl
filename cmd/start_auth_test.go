package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/api"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestResolveListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		auth    bool
		want    string
		wantErr bool
	}{
		{"auth keeps wildcard bind", ":8080", true, ":8080", false},
		{"no auth rewrites wildcard to loopback", ":8080", false, "127.0.0.1:8080", false},
		{"no auth rewrites 0.0.0.0", "0.0.0.0:9090", false, "127.0.0.1:9090", false},
		{"no auth keeps loopback", "127.0.0.1:8080", false, "127.0.0.1:8080", false},
		{"no auth keeps localhost", "localhost:8080", false, "localhost:8080", false},
		{"no auth keeps ipv6 loopback", "[::1]:8080", false, "[::1]:8080", false},
		{"invalid address", "8080", false, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveListenAddress(tc.address, tc.auth)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got (%q, %v), want (%q, err=%v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestLoadAPITokens(t *testing.T) {
	origToken, origFile := apiToken, apiTokenFile
	t.Cleanup(func() { apiToken, apiTokenFile = origToken, origFile })

	file := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(file, []byte("read viewer-tok\nsync ci-tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	apiToken, apiTokenFile = "", ""
	if tokens, err := loadAPITokens(); err != nil || len(tokens) != 0 {
		t.Fatalf("expected no tokens, got %v (%v)", tokens, err)
	}

	apiToken, apiTokenFile = "root-tok", file
	tokens, err := loadAPITokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens["root-tok"] != api.ScopeAdmin || tokens["viewer-tok"] != api.ScopeRead || tokens["ci-tok"] != api.ScopeSync {
		t.Fatalf("unexpected tokens: %v", tokens)
	}

	apiTokenFile = filepath.Join(t.TempDir(), "missing")
	if _, err := loadAPITokens(); err == nil {
		t.Fatal("expected error for missing token file")
	}

	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(bad, []byte("nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiTokenFile = bad
	if _, err := loadAPITokens(); err == nil {
		t.Fatal("expected error for malformed token file")
	}
}

func TestLoadAgentTokens(t *testing.T) {
	origFile := apiTokenFile
	t.Cleanup(func() { apiTokenFile = origFile })

	apiTokenFile = ""
	if agents, err := loadAgentTokens(); err != nil || len(agents) != 0 {
		t.Fatalf("expected none, got %v (%v)", agents, err)
	}

	file := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(file, []byte("admin root\nagent bot-tok claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiTokenFile = file
	agents, err := loadAgentTokens()
	if err != nil || len(agents) != 1 || agents["bot-tok"] != "claude" {
		t.Fatalf("unexpected agents %v (%v)", agents, err)
	}

	apiTokenFile = filepath.Join(t.TempDir(), "missing")
	if _, err := loadAgentTokens(); err == nil {
		t.Fatal("expected error for missing token file")
	}
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(bad, []byte("agent only-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiTokenFile = bad
	if _, err := loadAgentTokens(); err == nil {
		t.Fatal("expected error for malformed agent line")
	}
}

func TestNewSessionManager(t *testing.T) {
	origAgents, origSessions, origLogger := agentsFile, sessionsFile, logger
	t.Cleanup(func() { agentsFile, sessionsFile, logger = origAgents, origSessions, origLogger })
	logger = zap.NewNop()

	dir := t.TempDir()
	agentsFile = filepath.Join(dir, "agents.yaml")
	sessionsFile = filepath.Join(dir, "sessions.json")
	doc := "policy:\n  allowed_kinds: [ConfigMap]\n  allowed_namespaces: ['agent-*']\nagents:\n  - id: bot\n    cluster: dev\n    repos: [x]\n    max_sessions: 1\n"
	if err := os.WriteFile(agentsFile, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr, err := newSessionManager(cluster.NewClusters())
	if err != nil || mgr == nil {
		t.Fatalf("expected a manager, got %v", err)
	}
	if _, err := mgr.Create(context.Background(), "bot", 0); err == nil {
		t.Fatal("an unregistered cluster must fail session creation")
	}

	agentsFile = filepath.Join(dir, "missing.yaml")
	if _, err := newSessionManager(cluster.NewClusters()); err == nil {
		t.Fatal("expected error for a missing agents file")
	}
}
