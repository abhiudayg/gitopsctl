package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	adminTok = "admin-tok"
	alphaTok = "alpha-tok"
	betaTok  = "beta-tok"
	readTok  = "read-tok"
	syncTok  = "sync-tok"
)

type sessionView struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Namespace string `json:"namespace"`
	State     string `json:"state"`
	Commit    string `json:"commit"`
	Resources []struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"resources"`
}

type violationsView struct {
	Violations []struct {
		Rule string `json:"rule"`
	} `json:"violations"`
}

func (v violationsView) has(rule string) bool {
	for _, violation := range v.Violations {
		if violation.Rule == rule {
			return true
		}
	}
	return false
}

type agentEnv struct {
	*env
	repo   string
	agents string
	tokens string
	state  string
}

func newAgentEnv(t *testing.T, files map[string]string) *agentEnv {
	t.Helper()
	e := newEnv(t)
	e.useFakeCluster("dev")
	repo := newRepo(t, files)
	a := &agentEnv{env: e, repo: repo, state: e.configPath("sessions.json")}
	a.agents = a.writeAgents("")
	a.tokens = filepath.Join(e.root, "tokens")
	tokens := "admin " + adminTok + "\nread " + readTok + "\nsync " + syncTok + "\nagent " + alphaTok + " alpha\nagent " + betaTok + " beta\n"
	if err := os.WriteFile(a.tokens, []byte(tokens), 0o600); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *agentEnv) writeAgents(alphaExtra string) string {
	doc := fmt.Sprintf(`policy:
  allowed_kinds: [ConfigMap, apps/Deployment]
  allowed_namespaces: ["agent-*"]
  allowed_registries: [ghcr.io/acme]
  max_objects: 20
  max_replicas: 3
agents:
  - id: alpha
    cluster: dev
    repos: ["%s"]
    max_sessions: 2
    default_ttl: 10m
    max_ttl: 1h
    quota: {pods: "5"}
%s  - id: beta
    cluster: dev
    repos: ["%s"]
    max_sessions: 1
`, a.repo, alphaExtra, a.repo)
	path := filepath.Join(a.root, "agents.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		a.t.Fatal(err)
	}
	return path
}

func (a *agentEnv) startController() *proc {
	a.t.Helper()
	return a.start("--api-token-file", a.tokens, "--agents-file", a.agents, "--sessions-file", a.state)
}

func (a *agentEnv) create(p *proc, token string, body any) sessionView {
	a.t.Helper()
	status, raw := p.api(http.MethodPost, "/api/v1/sessions", token, body)
	if status != http.StatusCreated {
		a.t.Fatalf("create session: %d %s", status, raw)
	}
	var s sessionView
	if err := json.Unmarshal(raw, &s); err != nil {
		a.t.Fatal(err)
	}
	return s
}

func (a *agentEnv) deploy(p *proc, token, id string, body any) (int, []byte) {
	return p.api(http.MethodPost, "/api/v1/sessions/"+id+"/deploy", token, body)
}

func (a *agentEnv) deployOK(p *proc, token, id string) sessionView {
	a.t.Helper()
	status, raw := a.deploy(p, token, id, map[string]string{"repo": a.repo, "ref": "master", "path": "."})
	if status != http.StatusOK {
		a.t.Fatalf("deploy: %d %s", status, raw)
	}
	var s sessionView
	_ = json.Unmarshal(raw, &s)
	return s
}

func (a *agentEnv) get(p *proc, token, id string) (int, sessionView) {
	status, raw := p.api(http.MethodGet, "/api/v1/sessions/"+id, token, nil)
	var s sessionView
	_ = json.Unmarshal(raw, &s)
	return status, s
}

func deploymentManifest(name, namespace, image, securityContext string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels: {app: %s}
  template:
    metadata:
      labels: {app: %s}
    spec:
      containers:
      - name: app
        image: %s
        securityContext: %s
        resources:
          limits: {cpu: 100m, memory: 64Mi}
`, name, namespace, name, name, image, securityContext)
}

func TestScenario_S_AGENT_01_SessionLifecycle(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()

	s := a.create(p, alphaTok, map[string]string{})
	if s.Agent != "alpha" || s.State != "active" || !a.kube.NamespaceExists(s.Namespace) {
		t.Fatalf("unexpected session %+v", s)
	}
	labels := a.kube.NamespaceLabels(s.Namespace)
	if labels[managedByLabel] != "gitopsctl" || labels["gitopsctl.io/session"] != s.ID || labels["gitopsctl.io/agent"] != "alpha" {
		t.Fatalf("namespace labels %v", labels)
	}
	if _, ok := a.kube.getObject("resourcequotas", s.Namespace, "gitopsctl-session"); !ok {
		t.Fatal("quota should exist in the session namespace")
	}

	deployed := a.deployOK(p, alphaTok, s.ID)
	if len(deployed.Commit) != 40 || len(deployed.Resources) != 1 || deployed.Resources[0].Namespace != s.Namespace {
		t.Fatalf("deploy result %+v", deployed)
	}
	if _, ok := a.kube.Get(s.Namespace, "web"); !ok {
		t.Fatal("manifest was not applied into the session namespace")
	}
	if a.kube.Labels(s.Namespace, "web")["gitopsctl.io/app"] != "session-"+s.ID {
		t.Fatalf("applied object is not labelled as owned by the session: %v", a.kube.Labels(s.Namespace, "web"))
	}

	if status, _ := p.api(http.MethodDelete, "/api/v1/sessions/"+s.ID, alphaTok, nil); status != http.StatusOK {
		t.Fatalf("teardown status %d", status)
	}
	if a.kube.NamespaceExists(s.Namespace) || a.kube.CountIn(s.Namespace) != 0 {
		t.Fatal("teardown must remove the namespace and everything in it")
	}
	if status, _ := a.get(p, alphaTok, s.ID); status != http.StatusNotFound {
		t.Fatalf("session should be gone, got %d", status)
	}
}

func TestScenario_S_AGENT_02_RedeployPrunesDroppedResources(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"a.yaml": manifest("keep", "v1"), "b.yaml": manifest("drop", "v1")})
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})
	a.deployOK(p, alphaTok, s.ID)
	if _, ok := a.kube.Get(s.Namespace, "drop"); !ok {
		t.Fatal("setup: drop should exist")
	}

	commitFiles(t, a.repo, map[string]string{"a.yaml": manifest("keep", "v2")}, []string{"b.yaml"})
	a.deployOK(p, alphaTok, s.ID)

	if _, ok := a.kube.Get(s.Namespace, "drop"); ok {
		t.Fatal("a resource removed from git must be pruned from the session")
	}
	if a.kube.DataValue(s.Namespace, "keep", "key") != "v2" {
		obj, _ := a.kube.Get(s.Namespace, "keep")
		t.Fatalf("remaining resource must be updated: %v", obj)
	}
}

func TestScenario_S_AGENT_03_SessionsSurviveRestart(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})
	a.deployOK(p, alphaTok, s.ID)
	p.Stop()

	p2 := a.startController()
	status, got := a.get(p2, alphaTok, s.ID)
	if status != http.StatusOK || got.State != "active" || len(got.Resources) != 1 {
		t.Fatalf("session lost across restart: %d %+v", status, got)
	}
	if !a.kube.NamespaceExists(s.Namespace) {
		t.Fatal("namespace must survive a controller restart")
	}
	a.deployOK(p2, alphaTok, s.ID)
}

func TestScenario_S_AGENT_04_ExpiredSessionsAreRemovedAfterRestart(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()
	short := a.create(p, alphaTok, map[string]string{"ttl": "1s"})
	long := a.create(p, alphaTok, map[string]string{"ttl": "30m"})
	a.deployOK(p, alphaTok, short.ID)
	p.Stop()
	time.Sleep(1500 * time.Millisecond)

	p2 := a.startController()
	a.waitFor(10*time.Second, "expired session namespace removed", func() bool { return !a.kube.NamespaceExists(short.Namespace) })
	if status, _ := a.get(p2, alphaTok, short.ID); status != http.StatusNotFound {
		t.Fatalf("expired session record should be gone, got %d", status)
	}
	if !a.kube.NamespaceExists(long.Namespace) {
		t.Fatal("a session that has not expired must be kept")
	}
}

func TestScenario_F_AGENT_01_PolicyRejectsUnsafeWorkloads(t *testing.T) {
	cases := map[string]struct {
		manifest string
		rule     string
	}{
		"privileged":        {deploymentManifest("bad", "agent-alpha-x", "ghcr.io/acme/app:1", "{runAsNonRoot: true, privileged: true}"), "privileged"},
		"runs as root":      {deploymentManifest("bad", "agent-alpha-x", "ghcr.io/acme/app:1", "{}"), "run-as-non-root"},
		"foreign registry":  {deploymentManifest("bad", "agent-alpha-x", "evil.example/miner:1", "{runAsNonRoot: true}"), "image-registry"},
		"added capability":  {deploymentManifest("bad", "agent-alpha-x", "ghcr.io/acme/app:1", "{runAsNonRoot: true, capabilities: {add: [SYS_ADMIN]}}"), "capabilities"},
		"too many replicas": {strings.Replace(deploymentManifest("bad", "agent-alpha-x", "ghcr.io/acme/app:1", "{runAsNonRoot: true}"), "replicas: 1", "replicas: 50", 1), "max-replicas"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := newAgentEnv(t, map[string]string{"good.yaml": manifest("innocent", "1")})
			p := a.startController()
			s := a.create(p, alphaTok, map[string]string{})
			commitFiles(t, a.repo, map[string]string{"bad.yaml": strings.ReplaceAll(tc.manifest, "agent-alpha-x", s.Namespace)}, nil)

			status, raw := a.deploy(p, alphaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."})

			var body violationsView
			_ = json.Unmarshal(raw, &body)
			if status != http.StatusUnprocessableEntity || !body.has(tc.rule) {
				t.Fatalf("expected 422 with rule %q, got %d %s", tc.rule, status, raw)
			}
			if _, ok := a.kube.Get(s.Namespace, "innocent"); ok {
				t.Fatal("a rejected deploy must not apply the compliant objects either")
			}
			if _, ok := a.kube.getObject("deployments", s.Namespace, "bad"); ok {
				t.Fatal("the unsafe workload must never reach the cluster")
			}
		})
	}
}

func TestScenario_F_AGENT_02_PolicyBlocksClusterScopedAndOutOfScopeObjects(t *testing.T) {
	cases := map[string]struct {
		manifest string
		rule     string
	}{
		"cluster role binding": {"apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata:\n  name: pwn\nroleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}\nsubjects: [{kind: User, name: attacker}]\n", "kind"},
		"namespace":            {"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: agent-alpha-evil\n", "kind"},
		"other namespace":      {manifestIn("kube-system", "pwn", "1"), "namespace"},
		"reserved label":       {"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: spoof\n  labels: {gitopsctl.io/app: victim}\n", "reserved-label"},
		"kind not allowed":     {"apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n", "kind"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := newAgentEnv(t, map[string]string{"bad.yaml": tc.manifest})
			p := a.startController()
			s := a.create(p, alphaTok, map[string]string{})
			before := a.kube.Mutations()

			status, raw := a.deploy(p, alphaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."})

			var body violationsView
			_ = json.Unmarshal(raw, &body)
			if status != http.StatusUnprocessableEntity || len(body.Violations) == 0 {
				t.Fatalf("expected a policy rejection, got %d %s", status, raw)
			}
			if name != "kind not allowed" && !body.has(tc.rule) {
				t.Fatalf("expected rule %q in %s", tc.rule, raw)
			}
			if a.kube.Mutations() != before {
				t.Fatal("a rejected deploy must not write to the cluster")
			}
			if _, ok := a.kube.getObject("clusterrolebindings", "", "pwn"); ok {
				t.Fatal("cluster role binding was created")
			}
			if _, ok := a.kube.Get("kube-system", "pwn"); ok {
				t.Fatal("object leaked into kube-system")
			}
		})
	}
}

func TestScenario_F_AGENT_03_AgentTokensAreConfinedAndIsolated(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})
	a.deployOK(p, alphaTok, s.ID)

	for name, tc := range map[string]struct {
		method, path, token string
		want                int
	}{
		"beta reads alpha session":     {http.MethodGet, "/api/v1/sessions/" + s.ID, betaTok, http.StatusNotFound},
		"beta deletes alpha session":   {http.MethodDelete, "/api/v1/sessions/" + s.ID, betaTok, http.StatusNotFound},
		"agent lists applications":     {http.MethodGet, "/api/v1/applications", alphaTok, http.StatusForbidden},
		"agent registers application":  {http.MethodPost, "/api/v1/applications", alphaTok, http.StatusForbidden},
		"agent deletes cluster":        {http.MethodDelete, "/api/v1/clusters/dev", alphaTok, http.StatusForbidden},
		"agent reads events":           {http.MethodGet, "/api/v1/events/history", alphaTok, http.StatusForbidden},
		"read token lists sessions":    {http.MethodGet, "/api/v1/sessions", readTok, http.StatusForbidden},
		"sync token creates a session": {http.MethodPost, "/api/v1/sessions", syncTok, http.StatusForbidden},
		"anonymous lists sessions":     {http.MethodGet, "/api/v1/sessions", "", http.StatusUnauthorized},
		"wrong token":                  {http.MethodGet, "/api/v1/sessions", "guess", http.StatusUnauthorized},
	} {
		if status, body := p.api(tc.method, tc.path, tc.token, nil); status != tc.want {
			t.Errorf("%s: got %d (%s), want %d", name, status, body, tc.want)
		}
	}
	if status, body := a.deploy(p, betaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."}); status != http.StatusNotFound {
		t.Errorf("beta deploying into alpha's session: %d %s", status, body)
	}
	if status, _ := p.api(http.MethodPost, "/api/v1/sessions", alphaTok, map[string]string{"agent": "beta"}); status != http.StatusForbidden {
		t.Errorf("impersonation: %d", status)
	}
	if !a.kube.NamespaceExists(s.Namespace) {
		t.Fatal("alpha's namespace must be untouched")
	}
	if status, list := p.api(http.MethodGet, "/api/v1/sessions", betaTok, nil); status != http.StatusOK || strings.Contains(string(list), s.ID) {
		t.Errorf("beta's session list leaks alpha's session: %d %s", status, list)
	}
}

func TestScenario_F_AGENT_04_RepositoryPathAndRefAreConstrained(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	if err := os.Symlink("/etc", filepath.Join(a.root, "outside")); err != nil {
		t.Fatal(err)
	}
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})
	before := a.kube.Mutations()

	for name, tc := range map[string]struct {
		body map[string]string
		want int
	}{
		"repo not on allowlist": {map[string]string{"repo": "https://evil.example/x.git", "ref": "master", "path": "."}, http.StatusForbidden},
		"local path repo":       {map[string]string{"repo": "/etc", "ref": "master", "path": "."}, http.StatusForbidden},
		"parent path":           {map[string]string{"repo": a.repo, "ref": "master", "path": "../.."}, http.StatusBadRequest},
		"absolute path":         {map[string]string{"repo": a.repo, "ref": "master", "path": "/etc"}, http.StatusBadRequest},
		"option-like ref":       {map[string]string{"repo": a.repo, "ref": "--upload-pack=touch /tmp/pwn", "path": "."}, http.StatusBadRequest},
		"ref with dotdot":       {map[string]string{"repo": a.repo, "ref": "a..b", "path": "."}, http.StatusBadRequest},
	} {
		if status, body := a.deploy(p, alphaTok, s.ID, tc.body); status != tc.want {
			t.Errorf("%s: got %d (%s), want %d", name, status, body, tc.want)
		}
	}
	if a.kube.Mutations() != before {
		t.Fatal("rejected requests must not write to the cluster")
	}

	commitFiles(t, a.repo, map[string]string{}, nil)
	if err := os.Symlink("/etc/hosts", filepath.Join(a.repo, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	git(t, a.repo, "add", "-A")
	git(t, a.repo, "commit", "-m", "symlink")
	if status, body := a.deploy(p, alphaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."}); status != http.StatusBadRequest {
		t.Errorf("symlinked manifest: got %d %s", status, body)
	}
}

func TestScenario_F_AGENT_05_LimitsAreEnforced(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()

	a.create(p, betaTok, map[string]string{})
	if status, _ := p.api(http.MethodPost, "/api/v1/sessions", betaTok, map[string]string{}); status != http.StatusTooManyRequests {
		t.Errorf("session limit: got %d", status)
	}
	if status, _ := p.api(http.MethodPost, "/api/v1/sessions", alphaTok, map[string]string{"ttl": "5h"}); status != http.StatusBadRequest {
		t.Errorf("ttl above max: got %d", status)
	}
	if status, _ := p.api(http.MethodPost, "/api/v1/sessions", alphaTok, map[string]string{"ttl": "forever"}); status != http.StatusBadRequest {
		t.Errorf("malformed ttl: got %d", status)
	}
	if status, _ := p.api(http.MethodPost, "/api/v1/sessions", adminTok, map[string]string{"agent": "ghost"}); status != http.StatusForbidden {
		t.Errorf("unknown agent: got %d", status)
	}
	if got := len(a.kube.NamespaceNames()); got != 1 {
		t.Errorf("rejected requests created namespaces: %v", a.kube.NamespaceNames())
	}
}

func TestScenario_F_AGENT_06_CleanupNeverTouchesForeignNamespaces(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	a.kube.SeedNamespace("agent-alpha-deadbeef", map[string]string{managedByLabel: "gitopsctl", "gitopsctl.io/session": "deadbeef"})
	a.kube.SeedNamespace("agent-alpha-cafe0000", nil)
	a.kube.SeedNamespace("payments", map[string]string{"team": "payments"})
	a.kube.SeedNamespace("someone-elses", map[string]string{"gitopsctl.io/session": "abc123"})

	p := a.startController()

	t.Cleanup(func() {
		if t.Failed() {
			t.Log(p.Output())
		}
	})
	a.waitFor(10*time.Second, "orphaned session namespace removed", func() bool { return !a.kube.NamespaceExists("agent-alpha-deadbeef") })
	for _, ns := range []string{"agent-alpha-cafe0000", "payments", "someone-elses"} {
		if !a.kube.NamespaceExists(ns) {
			t.Errorf("namespace %q does not belong to a session and must never be deleted", ns)
		}
	}
}

func TestScenario_F_AGENT_07_InterruptedTeardownCompletesAfterOutage(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})
	a.deployOK(p, alphaTok, s.ID)

	a.kube.failMutationsWhen(func(method, _ string) bool { return method == http.MethodDelete })
	status, _ := p.api(http.MethodDelete, "/api/v1/sessions/"+s.ID, alphaTok, nil)
	if status != http.StatusAccepted {
		t.Fatalf("teardown during an outage should be accepted for retry, got %d", status)
	}
	if _, got := a.get(p, alphaTok, s.ID); got.State != "tearing-down" {
		t.Fatalf("state %q, want tearing-down", got.State)
	}
	if status, _ := a.deploy(p, alphaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."}); status != http.StatusConflict {
		t.Fatalf("a tearing-down session must reject deploys, got %d", status)
	}
	p.Stop()

	a.kube.failMutationsWhen(nil)
	p2 := a.startController()
	a.waitFor(10*time.Second, "interrupted teardown completed", func() bool { return !a.kube.NamespaceExists(s.Namespace) })
	if status, _ := a.get(p2, alphaTok, s.ID); status != http.StatusNotFound {
		t.Fatalf("session record should be gone, got %d", status)
	}
}

func TestScenario_F_AGENT_08_FailedApplyLeavesSessionUsable(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"a.yaml": manifest("first", "1"), "b.yaml": manifest("second", "1")})
	p := a.startController()
	s := a.create(p, alphaTok, map[string]string{})

	a.kube.failMutationsWhen(func(method, name string) bool { return method == http.MethodPost && name == "second" })
	status, body := a.deploy(p, alphaTok, s.ID, map[string]string{"repo": a.repo, "ref": "master", "path": "."})
	if status != http.StatusBadGateway || strings.Contains(string(body), "injected failure") {
		t.Fatalf("expected a generic 502 without internal detail, got %d %s", status, body)
	}
	if _, got := a.get(p, alphaTok, s.ID); got.State != "active" || got.Commit != "" {
		t.Fatalf("failed deploy must not be recorded: %+v", got)
	}

	a.kube.failMutationsWhen(nil)
	deployed := a.deployOK(p, alphaTok, s.ID)
	if len(deployed.Resources) != 2 {
		t.Fatalf("retry should apply both objects, got %+v", deployed)
	}
}

func TestScenario_F_AGENT_09_InvalidAgentConfigurationRefusesToStart(t *testing.T) {
	a := newAgentEnv(t, map[string]string{"cm.yaml": manifest("web", "v1")})
	valid, err := os.ReadFile(a.agents)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown field":           string(valid) + "unexpected: true\n",
		"no policy":               "agents:\n  - id: a\n    cluster: dev\n    repos: [x]\n    max_sessions: 1\n",
		"namespace not permitted": strings.Replace(string(valid), `allowed_namespaces: ["agent-*"]`, `allowed_namespaces: ["prod-*"]`, 1),
		"rbac kind allowed":       strings.Replace(string(valid), "allowed_kinds: [ConfigMap, apps/Deployment]", "allowed_kinds: [rbac.authorization.k8s.io/ClusterRoleBinding]", 1),
		"empty":                   "",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(a.root, "bad-agents.yaml")
			if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
				t.Fatal(err)
			}
			p := a.startNoWait("--api-token-file", a.tokens, "--agents-file", path, "--sessions-file", a.state)
			if !p.WaitExit(15*time.Second) || p.ExitCode() == 0 {
				t.Fatalf("controller must refuse to start with an invalid agents file:\n%s", p.Output())
			}
		})
	}
}
