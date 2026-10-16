package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	readToken  = "read-token-0123456789"
	syncToken  = "sync-token-0123456789"
	adminToken = "admin-token-0123456789"
)

func authedEnv(t *testing.T) (*env, *proc) {
	t.Helper()
	e := newEnv(t)
	e.useFakeCluster("c1")
	repo := newRepo(t, map[string]string{"cm.yaml": manifest("api-fixture", "v1")})
	e.writeApps(appSpec("web", repo, nil))

	tokenFile := e.configPath("tokens")
	content := fmt.Sprintf("# scoped tokens\nread %s\nsync %s\nadmin %s\n", readToken, syncToken, adminToken)
	if err := os.WriteFile(tokenFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return e, e.start("--api-token-file", tokenFile)
}

func TestScenario_S_API_01_HealthAndMetricsOpenWithoutToken(t *testing.T) {
	_, p := authedEnv(t)
	for _, path := range []string{"/health", "/metrics"} {
		if status, _ := p.api(http.MethodGet, path, "", nil); status != http.StatusOK {
			t.Errorf("GET %s without token = %d, want 200", path, status)
		}
	}
}

func TestScenario_S_API_02_ValidTokenCanListResources(t *testing.T) {
	_, p := authedEnv(t)

	status, body := p.api(http.MethodGet, "/api/v1/applications", adminToken, nil)
	var apps []map[string]any
	if status != http.StatusOK || json.Unmarshal(body, &apps) != nil || len(apps) != 1 || apps[0]["name"] != "web" {
		t.Fatalf("applications = %d %s", status, body)
	}
	status, body = p.api(http.MethodGet, "/api/v1/clusters", adminToken, nil)
	var clusters []map[string]any
	if status != http.StatusOK || json.Unmarshal(body, &clusters) != nil || len(clusters) != 1 {
		t.Fatalf("clusters = %d %s", status, body)
	}
}

func TestScenario_S_API_03_ScopesGateReadSyncAndAdminOperations(t *testing.T) {
	_, p := authedEnv(t)

	steps := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"read token can read", http.MethodGet, "/api/v1/applications", readToken, http.StatusOK},
		{"read token cannot sync", http.MethodPost, "/api/v1/applications/web/sync", readToken, http.StatusForbidden},
		{"sync token can sync", http.MethodPost, "/api/v1/applications/web/sync", syncToken, http.StatusAccepted},
		{"sync token cannot delete", http.MethodDelete, "/api/v1/applications/web", syncToken, http.StatusForbidden},
		{"admin token can delete", http.MethodDelete, "/api/v1/applications/web", adminToken, 0},
	}
	for _, step := range steps {
		status, body := p.api(step.method, step.path, step.token, nil)
		if step.want == 0 {
			if status < 200 || status > 299 {
				t.Errorf("%s: status %d (%s), want 2xx", step.name, status, body)
			}
			continue
		}
		if status != step.want {
			t.Errorf("%s: status %d (%s), want %d", step.name, status, body, step.want)
		}
	}
	if status, _ := p.api(http.MethodGet, "/api/v1/applications/web", adminToken, nil); status != http.StatusNotFound {
		t.Errorf("deleted application still present, status %d", status)
	}
}

func TestScenario_S_API_04_MutatingCallsAreAuditedWithoutLeakingTheToken(t *testing.T) {
	_, p := authedEnv(t)

	if status, _ := p.api(http.MethodPost, "/api/v1/applications/web/sync", syncToken, nil); status != http.StatusAccepted {
		t.Fatalf("sync status %d", status)
	}
	status, body := p.api(http.MethodGet, "/api/v1/events/history", readToken, nil)
	if status != http.StatusOK {
		t.Fatalf("history status %d", status)
	}
	if strings.Contains(string(body), syncToken) {
		t.Fatal("event history leaks the bearer token")
	}

	var envelopes []struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelopes); err != nil {
		t.Fatal(err)
	}
	for _, env := range envelopes {
		if env.Type == "io.gitopsctl.api.audit" && env.Data["method"] == "POST" && env.Data["path"] == "/api/v1/applications/web/sync" {
			if id, _ := env.Data["token_id"].(string); id == "" {
				t.Fatal("audit event lacks a token_id")
			}
			return
		}
	}
	t.Fatalf("no audit event found in %s", body)
}

func TestScenario_F_API_01_MissingOrInvalidCredentialsAreRejected(t *testing.T) {
	_, p := authedEnv(t)

	for name, token := range map[string]string{"missing": "", "wrong": "not-a-token"} {
		status, body := p.api(http.MethodGet, "/api/v1/applications", token, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s token: status %d, want 401", name, status)
		}
		if strings.Contains(string(body), "web") {
			t.Errorf("%s token: response leaks data: %s", name, body)
		}
	}
	if got := p.header(http.MethodGet, "/api/v1/applications", "", "WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}

	req, _ := http.NewRequest(http.MethodGet, p.url("/api/v1/applications"), nil)
	req.Header.Set("Authorization", "Basic "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("non-bearer scheme: status %d, want 401", resp.StatusCode)
	}
}

func TestScenario_F_API_02_InsufficientScopeLeavesStateUntouched(t *testing.T) {
	_, p := authedEnv(t)

	if status, _ := p.api(http.MethodDelete, "/api/v1/applications/web", readToken, nil); status != http.StatusForbidden {
		t.Fatalf("read token DELETE = %d, want 403", status)
	}
	if status, _ := p.api(http.MethodGet, "/api/v1/applications/web", adminToken, nil); status != http.StatusOK {
		t.Fatalf("application must still exist, status %d", status)
	}
}

func TestScenario_F_API_03_MalformedAndUnknownRequestsFailCleanly(t *testing.T) {
	_, p := authedEnv(t)

	for _, path := range []string{"/api/v1/applications/ghost/sync", "/api/v1/applications/ghost/plan"} {
		if status, _ := p.api(http.MethodPost, path, adminToken, nil); status != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, status)
		}
	}
	if status, _ := p.api(http.MethodPost, "/api/v1/applications/ghost/approve", adminToken, map[string]string{"commitHash": "abc"}); status != http.StatusNotFound {
		t.Errorf("approve unknown app = %d, want 404", status)
	}
	if status, _ := p.api(http.MethodGet, "/api/v1/applications/ghost", adminToken, nil); status != http.StatusNotFound {
		t.Errorf("GET unknown app = %d, want 404", status)
	}

	req, _ := http.NewRequest(http.MethodPost, p.url("/api/v1/applications"), strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON registration = %d, want 400", resp.StatusCode)
	}
	if !p.Alive() {
		t.Fatalf("controller died:\n%s", p.Output())
	}
}

func TestScenario_F_SEC_01_SecretsNeverAppearInProcessOutput(t *testing.T) {
	e, p := authedEnv(t)

	p.api(http.MethodGet, "/api/v1/applications", adminToken, nil)
	p.api(http.MethodGet, "/api/v1/applications", "wrong-"+adminToken, nil)
	p.api(http.MethodPost, "/api/v1/applications/web/sync", syncToken, nil)
	cliOut, _ := e.run("--api-url", p.url(""), "--api-token", adminToken, "diff-app", "-n", "web")
	time.Sleep(500 * time.Millisecond)

	for _, secret := range []string{readToken, syncToken, adminToken} {
		if strings.Contains(p.Output(), secret) {
			t.Errorf("controller output leaks %q", secret)
		}
		if strings.Contains(cliOut, secret) {
			t.Errorf("CLI output leaks %q", secret)
		}
	}
}

func TestScenario_S_START_01_WithoutTokenTheAPIBindsToLoopbackOnly(t *testing.T) {
	e := newEnv(t)
	port := freePort(t)
	p := e.launch(port, []string{"start", "--api-address", fmt.Sprintf(":%d", port)})

	e.waitFor(20*time.Second, "controller healthy on loopback", func() bool {
		status, _ := p.api(http.MethodGet, "/health", "", nil)
		return status == http.StatusOK
	})
	if !strings.Contains(p.Output(), "binding API to loopback only") {
		t.Errorf("expected loopback warning in output:\n%s", p.Output())
	}

	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipNet.IP.String(), fmt.Sprint(port)), time.Second)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("API reachable on non-loopback address %s without a token", ipNet.IP)
		}
		return
	}
}

func TestScenario_F_START_01_CorruptStateFileStopsStartupWithoutOverwritingIt(t *testing.T) {
	e := newEnv(t)
	corrupt := `[{"name":"web","interval":"5m"`
	if err := os.WriteFile(e.configPath("applications.json"), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}

	p := e.startNoWait()
	if !p.WaitExit(15 * time.Second) {
		t.Fatalf("controller kept running with corrupt state:\n%s", p.Output())
	}
	if p.ExitCode() == 0 || !strings.Contains(p.Output(), "failed to load applications") {
		t.Errorf("exit=%d output:\n%s", p.ExitCode(), p.Output())
	}
	got, _ := os.ReadFile(e.configPath("applications.json"))
	if string(got) != corrupt {
		t.Errorf("corrupt state file was modified: %q", got)
	}
}

func TestScenario_F_START_02_MalformedTokenFileStopsStartup(t *testing.T) {
	e := newEnv(t)
	tokenFile := e.configPath("tokens")
	if err := os.WriteFile(tokenFile, []byte("bogus-line-without-scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := e.startNoWait("--api-token-file", tokenFile)
	if !p.WaitExit(15*time.Second) || p.ExitCode() == 0 {
		t.Fatalf("expected startup failure, output:\n%s", p.Output())
	}
	if !strings.Contains(p.Output(), "token file") {
		t.Errorf("error should mention the token file:\n%s", p.Output())
	}
}
