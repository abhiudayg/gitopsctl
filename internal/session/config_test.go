package session

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfig_AppliesDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	beta, ok := cfg.Agent("beta")
	if !ok || time.Duration(beta.DefaultTTL) != defaultTTL || time.Duration(beta.MaxTTL) != maxTTL {
		t.Fatalf("defaults not applied: %+v", beta)
	}
	if _, ok := cfg.Agent("missing"); ok {
		t.Fatal("unknown agent must not resolve")
	}
}

func TestParseConfig_FailsClosed(t *testing.T) {
	policy := "policy:\n  allowed_kinds: [ConfigMap]\n  allowed_namespaces: ['agent-*']\n"
	agent := func(extra string) string {
		return policy + "agents:\n  - id: a\n    cluster: dev\n    repos: [x]\n    max_sessions: 1\n" + extra
	}
	cases := map[string]string{
		"missing policy":          "agents:\n  - id: a\n    cluster: dev\n    repos: [x]\n    max_sessions: 1\n",
		"invalid policy":          "policy:\n  allowed_kinds: []\nagents:\n  - id: a\n",
		"no agents":               policy,
		"bad id":                  strings.Replace(agent(""), "id: a", "id: Bad_ID", 1),
		"id too long":             strings.Replace(agent(""), "id: a", "id: aaaaaaaaaaaaaaaaaaaaaaaaa", 1),
		"duplicate id":            agent("") + "  - id: a\n    cluster: dev\n    repos: [x]\n    max_sessions: 1\n",
		"no cluster":              strings.Replace(agent(""), "cluster: dev", "cluster: ''", 1),
		"no repos":                strings.Replace(agent(""), "repos: [x]", "repos: []", 1),
		"blank repo":              strings.Replace(agent(""), "repos: [x]", "repos: [' ']", 1),
		"no max sessions":         strings.Replace(agent(""), "max_sessions: 1", "max_sessions: 0", 1),
		"ttl above max":           agent("    default_ttl: 2h\n    max_ttl: 1h\n"),
		"negative ttl":            agent("    default_ttl: -5m\n"),
		"bad duration":            agent("    default_ttl: soon\n"),
		"duration not string":     agent("    default_ttl: 30\n"),
		"unknown field":           agent("    admin: true\n"),
		"namespace not permitted": strings.Replace(agent(""), "allowed_namespaces: ['agent-*']", "allowed_namespaces: ['other-*']", 1),
		"not yaml":                "::: nope",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(doc)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/agents.yaml"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestAllowsRepo(t *testing.T) {
	cfg, _ := ParseConfig([]byte(testConfig))
	alpha, _ := cfg.Agent("alpha")
	for repo, want := range map[string]bool{
		"https://git.example/acme/app":      true,
		"https://git.example/exact":         true,
		"https://git.example/exact-evil":    false,
		"https://git.example/acme":          false,
		"https://git.example/acme-evil/app": false,
		"https://evil.example/acme/app":     false,
		"":                                  false,
	} {
		if got := alpha.allowsRepo(repo); got != want {
			t.Errorf("allowsRepo(%q) = %v, want %v", repo, got, want)
		}
	}
}
