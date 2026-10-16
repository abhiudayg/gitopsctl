package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/api"
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
