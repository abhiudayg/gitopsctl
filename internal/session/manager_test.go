package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/policy"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

func TestCreate_ProvisionsNamespaceAndPersistsPrivately(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")

	if s.State != StateActive || s.Namespace != NamespaceName("alpha", s.ID) || !h.cluster.has(s.Namespace) {
		t.Fatalf("unexpected session %+v", s)
	}
	if got := s.ExpiresAt.Sub(s.CreatedAt); got != 30*time.Minute {
		t.Fatalf("default ttl not applied: %s", got)
	}
	info, err := os.Stat(h.file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode %v, want 0600", info.Mode().Perm())
	}
}

func TestCreate_Rejections(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.mgr.Create(ctx, "ghost", 0); !isErr(err, ErrForbidden) {
		t.Fatalf("unknown agent: %v", err)
	}
	if _, err := h.mgr.Create(ctx, "alpha", 2*time.Hour); !isErr(err, ErrInvalid) {
		t.Fatalf("ttl above max: %v", err)
	}
	if _, err := h.mgr.Create(ctx, "alpha", -time.Minute); !isErr(err, ErrInvalid) {
		t.Fatalf("negative ttl: %v", err)
	}
	h.create("beta")
	if _, err := h.mgr.Create(ctx, "beta", 0); !isErr(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
}

func TestCreate_ConcurrentRequestsRespectLimit(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.mgr.Create(context.Background(), "alpha", 0); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if succeeded != 2 {
		t.Fatalf("max_sessions is 2 but %d sessions were created", succeeded)
	}
}

func TestCreate_FailedProvisioningIsCleanedUpByReap(t *testing.T) {
	h := newHarness(t)
	h.cluster.ensureErr = errors.New("api down")
	if _, err := h.mgr.Create(context.Background(), "alpha", 0); err == nil {
		t.Fatal("expected an error")
	}
	pending := h.mgr.List("alpha")
	if len(pending) != 1 || pending[0].State != StateCreating {
		t.Fatalf("expected a creating record to be kept for cleanup, got %+v", pending)
	}

	h.cluster.ensureErr = nil
	h.mgr.Reap(context.Background())
	if left := h.mgr.List(""); len(left) != 0 {
		t.Fatalf("stale creating session should be reaped, got %+v", left)
	}
}

func TestSessions_AreIsolatedBetweenAgents(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	ctx := context.Background()

	if _, err := h.mgr.Get(s.ID, "beta"); !isErr(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if _, err := h.mgr.Deploy(ctx, s.ID, "beta", okRequest()); !isErr(err, ErrNotFound) {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := h.mgr.Teardown(ctx, s.ID, "beta"); !isErr(err, ErrNotFound) {
		t.Fatalf("teardown: %v", err)
	}
	if len(h.mgr.List("beta")) != 0 || len(h.mgr.List("alpha")) != 1 || len(h.mgr.List("")) != 1 {
		t.Fatal("list must be scoped to the caller; the empty agent sees all")
	}
	if !h.cluster.has(s.Namespace) {
		t.Fatal("another agent must not be able to remove the namespace")
	}
	if _, err := h.mgr.Get(s.ID, ""); err != nil {
		t.Fatalf("admin access: %v", err)
	}
	if _, err := h.mgr.Get("nope", ""); !isErr(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestDeploy_AppliesInsideSessionNamespaceAndTracksResources(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")

	deployed, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest())
	if err != nil {
		t.Fatal(err)
	}
	if deployed.Commit != "abc123" || deployed.Ref != "feature/x" || len(deployed.Resources) != 1 {
		t.Fatalf("unexpected result %+v", deployed)
	}
	if h.cluster.applies[0].Namespace != s.Namespace || len(h.cluster.applies[0].Previous) != 0 {
		t.Fatalf("unexpected apply %+v", h.cluster.applies[0])
	}

	if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest()); err != nil {
		t.Fatal(err)
	}
	if len(h.cluster.applies[1].Previous) != 1 {
		t.Fatal("a redeploy must hand the previous resources to the apply so stale ones are pruned")
	}
}

func TestDeploy_Rejections(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	link := filepath.Join(h.repoDir, "escape")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		req  DeployRequest
		want error
	}{
		"repo not allowed":    {DeployRequest{Repo: "https://evil.example/x", Ref: "main", Path: "."}, ErrForbidden},
		"repo of other agent": {DeployRequest{Repo: "https://git.example/beta/app", Ref: "main", Path: "."}, ErrForbidden},
		"local file repo":     {DeployRequest{Repo: "/etc", Ref: "main", Path: "."}, ErrForbidden},
		"absolute path":       {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: "/etc"}, ErrInvalid},
		"parent path":         {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: "../x"}, ErrInvalid},
		"sneaky parent":       {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: "a/../../x"}, ErrInvalid},
		"bare parent":         {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: ".."}, ErrInvalid},
		"symlinked directory": {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: "escape"}, ErrInvalid},
		"missing path":        {DeployRequest{Repo: "https://git.example/acme/a", Ref: "main", Path: "nope"}, ErrInvalid},
		"option-like ref":     {DeployRequest{Repo: "https://git.example/acme/a", Ref: "--upload-pack=x", Path: "."}, ErrInvalid},
		"dotdot ref":          {DeployRequest{Repo: "https://git.example/acme/a", Ref: "a..b", Path: "."}, ErrInvalid},
		"empty ref":           {DeployRequest{Repo: "https://git.example/acme/a", Ref: "", Path: "."}, ErrInvalid},
		"space in ref":        {DeployRequest{Repo: "https://git.example/acme/a", Ref: "a b", Path: "."}, ErrInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", tc.req); !isErr(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(h.cluster.applies) != 0 {
				t.Fatal("a rejected request must never reach the cluster")
			}
		})
	}
}

func TestDeploy_SymlinkInsideManifestsIsRejected(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	if err := os.Symlink("/etc/hosts", filepath.Join(h.repoDir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest()); !isErr(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestDeploy_RequiresActiveUnexpiredSession(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	h.now = h.now.Add(31 * time.Minute)
	if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest()); !isErr(err, ErrNotActive) {
		t.Fatalf("expired: %v", err)
	}
}

func TestDeploy_PropagatesPolicyViolations(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	h.cluster.applyErr = fmt.Errorf("apply: %w", errors.Join(policy.Violation{Rule: "privileged", Message: "no"}))
	_, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest())
	var violation policy.Violation
	if !errors.As(err, &violation) || violation.Rule != "privileged" {
		t.Fatalf("expected the violation to be visible to callers, got %v", err)
	}
	if got, _ := h.mgr.Get(s.ID, "alpha"); len(got.Resources) != 0 || got.Commit != "" {
		t.Fatal("a failed deploy must not be recorded")
	}
}

func TestDeploy_FetchFailure(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	h.mgr.fetch = func(context.Context, string, string) (string, string, func(), error) {
		return "", "", nil, errors.New("clone failed")
	}
	if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDeploy_SerialisesConcurrentRequestsForOneSession(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	h.cluster.applyDelay = 20 * time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest())
		}()
	}
	wg.Wait()
	if h.cluster.maxInFlight != 1 {
		t.Fatalf("deploys overlapped: %d in flight", h.cluster.maxInFlight)
	}
}

func TestTeardown_RemovesNamespaceAndRecord(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	if _, err := h.mgr.Teardown(context.Background(), s.ID, "alpha"); err != nil {
		t.Fatal(err)
	}
	if h.cluster.has(s.Namespace) || len(h.mgr.List("")) != 0 {
		t.Fatal("namespace and record should be gone")
	}
}

func TestTeardown_FailureIsRetriedByReap(t *testing.T) {
	h := newHarness(t)
	s := h.create("alpha")
	h.cluster.deleteErr = errors.New("api down")
	if _, err := h.mgr.Teardown(context.Background(), s.ID, "alpha"); err == nil {
		t.Fatal("expected an error")
	}
	got, _ := h.mgr.Get(s.ID, "alpha")
	if got.State != StateTearingDown {
		t.Fatalf("state %s, want tearing-down", got.State)
	}
	if _, err := h.mgr.Deploy(context.Background(), s.ID, "alpha", okRequest()); !isErr(err, ErrNotActive) {
		t.Fatalf("a tearing-down session must not accept deploys: %v", err)
	}

	h.cluster.deleteErr = nil
	h.mgr.Reap(context.Background())
	if h.cluster.has(s.Namespace) || len(h.mgr.List("")) != 0 {
		t.Fatal("reap should finish the interrupted teardown")
	}
}

func TestReap_ExpiredSessionsOnly(t *testing.T) {
	h := newHarness(t)
	short, err := h.mgr.Create(context.Background(), "alpha", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	long := h.create("alpha")
	h.now = h.now.Add(5 * time.Minute)

	h.mgr.Reap(context.Background())

	if h.cluster.has(short.Namespace) || !h.cluster.has(long.Namespace) {
		t.Fatal("only the expired session may be removed")
	}
	if got := h.mgr.List(""); len(got) != 1 || got[0].ID != long.ID {
		t.Fatalf("unexpected sessions %+v", got)
	}
}

func TestReap_DeletesOrphanedNamespacesButKeepsKnownOnes(t *testing.T) {
	h := newHarness(t)
	known := h.create("alpha")
	h.cluster.namespaces["agent-alpha-deadbeef"] = "deadbeef"

	h.mgr.Reap(context.Background())

	if h.cluster.has("agent-alpha-deadbeef") {
		t.Fatal("orphan should be deleted")
	}
	if !h.cluster.has(known.Namespace) {
		t.Fatal("namespace with a session record must be kept")
	}
}

func TestReap_ListFailureDeletesNothing(t *testing.T) {
	h := newHarness(t)
	h.cluster.namespaces["agent-alpha-deadbeef"] = "deadbeef"
	h.cluster.listErr = errors.New("api down")
	h.mgr.Reap(context.Background())
	if !h.cluster.has("agent-alpha-deadbeef") {
		t.Fatal("nothing may be deleted when the scan failed")
	}
}

func TestRestart_RecoversSessionsAndCleansInterruptedOnes(t *testing.T) {
	h := newHarness(t)
	active := h.create("alpha")
	stuck := h.create("alpha")
	h.cluster.deleteErr = errors.New("api down")
	_, _ = h.mgr.Teardown(context.Background(), stuck.ID, "alpha")
	h.cluster.deleteErr = nil

	restarted := h.newManager()
	if got, err := restarted.Get(active.ID, "alpha"); err != nil || got.State != StateActive {
		t.Fatalf("active session lost: %v %+v", err, got)
	}
	restarted.Reap(context.Background())
	if h.cluster.has(stuck.Namespace) {
		t.Fatal("the interrupted teardown should complete after a restart")
	}
	if !h.cluster.has(active.Namespace) {
		t.Fatal("the active session must survive a restart")
	}
}

func TestNewManager_CorruptStateFails(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := ParseConfig([]byte(testConfig))
	if _, err := NewManager(zap.NewNop(), cfg, func(string) (Cluster, error) { return h.cluster, nil }, h.file); err == nil {
		t.Fatal("expected an error for a corrupt sessions file")
	}
}

func TestRun_ReapsImmediatelyAndStopsOnCancel(t *testing.T) {
	h := newHarness(t)
	h.cluster.namespaces["agent-alpha-deadbeef"] = "deadbeef"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.mgr.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for h.cluster.has("agent-alpha-deadbeef") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if h.cluster.has("agent-alpha-deadbeef") {
		t.Fatal("run should reap on start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestClusterLookupFailure(t *testing.T) {
	h := newHarness(t)
	cfg, _ := ParseConfig([]byte(testConfig))
	mgr, err := NewManager(zap.NewNop(), cfg, func(string) (Cluster, error) { return nil, errors.New("unknown cluster") }, filepath.Join(t.TempDir(), "s.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Create(context.Background(), "alpha", 0); err == nil {
		t.Fatal("expected an error")
	}
	_ = h
}

func TestGitFetcher_ClonesLocalRepository(t *testing.T) {
	src := t.TempDir()
	repo, err := gogit.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.yaml"), []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("a.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	head, _ := repo.Head()
	branch := head.Name().Short()

	dir, commit, cleanup, err := gitFetcher(zap.NewNop())(context.Background(), src, branch)
	if err != nil {
		t.Fatal(err)
	}
	if commit != head.Hash().String() {
		t.Fatalf("commit %s, want %s", commit, head.Hash())
	}
	if _, err := os.Stat(filepath.Join(dir, "a.yaml")); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("cleanup should remove the clone")
	}

	if _, _, _, err := gitFetcher(zap.NewNop())(context.Background(), src, "no-such-branch"); err == nil {
		t.Fatal("expected an error for an unknown branch")
	}
}
