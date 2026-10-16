package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const settle = 30 * time.Second

func syncedEnv(t *testing.T, files map[string]string, extra map[string]any, allowed ...string) (*env, *proc, string) {
	t.Helper()
	e := newEnv(t)
	e.useFakeCluster("c1", allowed...)
	repo := newRepo(t, files)
	e.writeApps(appSpec("web", repo, extra))
	return e, e.start(), repo
}

func isSynced(st appState) bool { return st.Status == "Synced" || st.Status == "Healthy" }

func TestScenario_S_SYNC_01_ManifestsAreAppliedWithOwnershipLabels(t *testing.T) {
	e, p, repo := syncedEnv(t, map[string]string{"cm.yaml": manifest("app-config", "v1")}, nil)

	st := e.waitForApp(p, "", "web", settle, "app synced", isSynced)
	if got := e.kube.DataValue("default", "app-config", "key"); got != "v1" {
		t.Fatalf("live data = %q, want v1", got)
	}
	labels := e.kube.Labels("default", "app-config")
	if labels[managedByLabel] != "gitopsctl" || labels[appLabel] != "web" || labels[clusterLabel] != "c1" {
		t.Fatalf("ownership labels missing: %v", labels)
	}
	if head := git(t, repo, "rev-parse", "HEAD"); st.LastSynced != head {
		t.Fatalf("last synced %s, want repo HEAD %s", st.LastSynced, head)
	}
	if st.Failures != 0 {
		t.Fatalf("consecutive failures = %d", st.Failures)
	}
}

func TestScenario_S_SYNC_02_UnchangedCommitCausesNoFurtherClusterWrites(t *testing.T) {
	e, p, _ := syncedEnv(t, map[string]string{"cm.yaml": manifest("app-config", "v1")}, nil)
	e.waitForApp(p, "", "web", settle, "app synced", isSynced)

	before := e.kube.Mutations()
	time.Sleep(3500 * time.Millisecond)
	if after := e.kube.Mutations(); after != before {
		t.Fatalf("cluster was written %d more time(s) while the commit was unchanged", after-before)
	}
}

func TestScenario_S_SYNC_03_NewCommitPropagatesToTheCluster(t *testing.T) {
	e, p, repo := syncedEnv(t, map[string]string{"cm.yaml": manifest("app-config", "v1")}, nil)
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)

	head := commitFiles(t, repo, map[string]string{"cm.yaml": manifest("app-config", "v2")}, nil)
	e.waitForApp(p, "", "web", settle, "new commit synced", func(st appState) bool { return isSynced(st) && st.LastSynced == head })
	if got := e.kube.DataValue("default", "app-config", "key"); got != "v2" {
		t.Fatalf("live data = %q, want v2", got)
	}
}

func TestScenario_S_SYNC_04_ManualPolicyHoldsChangesUntilApproved(t *testing.T) {
	e, p, _ := syncedEnv(t, map[string]string{"cm.yaml": manifest("app-config", "v1")}, map[string]any{"sync_policy": "manual"})

	st := e.waitForApp(p, "", "web", settle, "out of sync awaiting approval", func(st appState) bool { return st.Status == "OutOfSync" })
	if len(e.kube.Names()) != 0 {
		t.Fatalf("manual policy applied resources before approval: %v", e.kube.Names())
	}

	out, code := e.run("--api-url", p.url(""), "approve-app", "-n", "web", "-c", st.LatestHash)
	if code != 0 {
		t.Fatalf("approve-app failed (%d): %s", code, out)
	}
	e.waitForApp(p, "", "web", settle, "synced after approval", isSynced)
	if _, ok := e.kube.Get("default", "app-config"); !ok {
		t.Fatal("approved commit was not applied")
	}
}

func TestScenario_S_PLAN_01_DiffReportsChangesWithoutWritingToTheCluster(t *testing.T) {
	e, p, repo := syncedEnv(t, map[string]string{"cm.yaml": manifest("app-config", "v1")}, map[string]any{"sync_policy": "manual"})

	st := e.waitForApp(p, "", "web", settle, "awaiting approval", func(st appState) bool { return st.Status == "OutOfSync" })
	out, code := e.run("--api-url", p.url(""), "diff-app", "-n", "web")
	if code != 0 || !strings.Contains(out, "+ create") || !strings.Contains(out, "app-config") {
		t.Fatalf("expected create plan (exit %d):\n%s", code, out)
	}
	if len(e.kube.Names()) != 0 {
		t.Fatalf("plan created resources: %v", e.kube.Names())
	}

	if _, code := e.run("--api-url", p.url(""), "approve-app", "-n", "web", "-c", st.LatestHash); code != 0 {
		t.Fatal("approve-app failed")
	}
	e.waitForApp(p, "", "web", settle, "synced", isSynced)

	out, _ = e.run("--api-url", p.url(""), "diff-app", "-n", "web")
	if !strings.Contains(out, "= unchanged") || !strings.Contains(out, "0 to create, 0 to update") {
		t.Fatalf("expected in-sync plan:\n%s", out)
	}

	commitFiles(t, repo, map[string]string{"cm.yaml": manifest("app-config", "v2"), "extra.yaml": manifest("extra", "x")}, nil)
	e.waitForApp(p, "", "web", settle, "new commit detected", func(st appState) bool { return st.Status == "OutOfSync" })

	before := e.kube.Mutations()
	out, code = e.run("--api-url", p.url(""), "diff-app", "-n", "web")
	if code != 0 || !strings.Contains(out, "~ update") || !strings.Contains(out, "+ create") || !strings.Contains(out, "1 to create, 1 to update") {
		t.Fatalf("expected update+create plan (exit %d):\n%s", code, out)
	}
	if after := e.kube.Mutations(); after != before {
		t.Fatalf("diff-app wrote to the cluster (%d mutations)", after-before)
	}
	if got := e.kube.DataValue("default", "app-config", "key"); got != "v1" {
		t.Fatalf("live object changed by plan: %q", got)
	}
}

func TestScenario_F_SYNC_01_ClusterOutageIsReportedAndRecoveredFrom(t *testing.T) {
	e := newEnv(t)
	e.useFakeCluster("c1")
	e.kube.failMutationsWhen(func(string, string) bool { return true })
	repo := newRepo(t, map[string]string{"cm.yaml": manifest("app-config", "v1")})
	e.writeApps(appSpec("web", repo, nil))
	p := e.start()

	st := e.waitForApp(p, "", "web", settle, "error reported", func(st appState) bool { return st.Status == "Error" })
	if st.Failures < 1 || !strings.Contains(st.Message, "Apply error") {
		t.Fatalf("failure not recorded: %+v", st)
	}
	if !p.Alive() {
		t.Fatalf("controller died during outage:\n%s", p.Output())
	}

	e.kube.failMutationsWhen(nil)
	e.waitForApp(p, "", "web", settle, "recovered after outage", isSynced)
	if _, ok := e.kube.Get("default", "app-config"); !ok {
		t.Fatal("resource not applied after recovery")
	}
}

func TestScenario_F_SYNC_02_UnreachableGitRepositoryIsReportedAndRecoveredFrom(t *testing.T) {
	e := newEnv(t)
	e.useFakeCluster("c1")
	later := filepath.Join(e.root, "repo-appears-later")
	e.writeApps(appSpec("web", later, nil))
	p := e.start()

	st := e.waitForApp(p, "", "web", settle, "git error reported", func(st appState) bool { return st.Status == "Error" })
	if !strings.Contains(st.Message, "Git error") || st.Failures < 1 {
		t.Fatalf("git failure not recorded: %+v", st)
	}

	built := newRepo(t, map[string]string{"cm.yaml": manifest("app-config", "v1")})
	if err := os.Rename(built, later); err != nil {
		t.Fatal(err)
	}
	e.waitForApp(p, "", "web", settle, "recovered once git is reachable", isSynced)
	if _, ok := e.kube.Get("default", "app-config"); !ok {
		t.Fatal("resource not applied after git recovered")
	}
}

func TestScenario_F_SYNC_03_NamespaceRestrictionBlocksForbiddenResources(t *testing.T) {
	e, p, _ := syncedEnv(t, map[string]string{"cm.yaml": manifestIn("prod", "intruder", "v")}, nil, "demo")

	st := e.waitForApp(p, "", "web", settle, "error for forbidden namespace", func(st appState) bool { return st.Status == "Error" })
	if !strings.Contains(st.Message, "not in the allowed list") {
		t.Fatalf("message should explain the restriction: %q", st.Message)
	}
	if len(e.kube.Names()) != 0 {
		t.Fatalf("resource leaked into a forbidden namespace: %v", e.kube.Names())
	}
}
