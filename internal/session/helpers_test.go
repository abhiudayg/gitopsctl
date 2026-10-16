package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

const testConfig = `
policy:
  allowed_kinds: [ConfigMap]
  allowed_namespaces: ["agent-*"]
agents:
  - id: alpha
    cluster: dev
    repos: ["https://git.example/acme/", "https://git.example/exact"]
    max_sessions: 2
    default_ttl: 30m
    max_ttl: 1h
    quota: {pods: "5"}
  - id: beta
    cluster: dev
    repos: ["https://git.example/beta/"]
    max_sessions: 1
`

type fakeCluster struct {
	mu          sync.Mutex
	namespaces  map[string]string
	applies     []ApplyRequest
	ensureErr   error
	deleteErr   error
	listErr     error
	applyErr    error
	inFlight    int
	maxInFlight int
	applyDelay  time.Duration
}

func newFakeCluster() *fakeCluster { return &fakeCluster{namespaces: map[string]string{}} }

func (f *fakeCluster) EnsureNamespace(_ context.Context, spec k8s.SessionNamespaceSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.namespaces[spec.Name] = spec.Session
	return nil
}

func (f *fakeCluster) DeleteNamespace(_ context.Context, name, session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if owner, ok := f.namespaces[name]; ok && owner != session {
		return k8s.ErrNamespaceNotOwned
	}
	delete(f.namespaces, name)
	return nil
}

func (f *fakeCluster) Namespaces(context.Context) ([]k8s.SessionNamespace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []k8s.SessionNamespace
	for name, session := range f.namespaces {
		out = append(out, k8s.SessionNamespace{Name: name, Session: session})
	}
	return out, nil
}

func (f *fakeCluster) Apply(_ context.Context, req ApplyRequest) ([]k8s.ResourceMetadata, error) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.applies = append(f.applies, req)
	delay, err := f.applyDelay, f.applyErr
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return []k8s.ResourceMetadata{{Version: "v1", Kind: "ConfigMap", Namespace: req.Namespace, Name: "cm"}}, nil
}

func (f *fakeCluster) has(ns string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.namespaces[ns]
	return ok
}

type harness struct {
	t       *testing.T
	cluster *fakeCluster
	file    string
	now     time.Time
	mgr     *Manager
	repoDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, cluster: newFakeCluster(), file: filepath.Join(t.TempDir(), "sessions.json"), now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), repoDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(h.repoDir, "cm.yaml"), []byte("kind: ConfigMap\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.mgr = h.newManager()
	return h
}

func (h *harness) newManager() *Manager {
	h.t.Helper()
	cfg, err := ParseConfig([]byte(testConfig))
	if err != nil {
		h.t.Fatal(err)
	}
	mgr, err := NewManager(zap.NewNop(), cfg, func(string) (Cluster, error) { return h.cluster, nil }, h.file,
		WithClock(func() time.Time { return h.now }),
		WithFetcher(func(context.Context, string, string) (string, string, func(), error) {
			return h.repoDir, "abc123", func() {}, nil
		}))
	if err != nil {
		h.t.Fatal(err)
	}
	return mgr
}

func (h *harness) create(agent string) Session {
	h.t.Helper()
	s, err := h.mgr.Create(context.Background(), agent, 0)
	if err != nil {
		h.t.Fatalf("create: %v", err)
	}
	return s
}

func okRequest() DeployRequest {
	return DeployRequest{Repo: "https://git.example/acme/app", Ref: "feature/x", Path: "."}
}

func isErr(err, target error) bool { return errors.Is(err, target) }
