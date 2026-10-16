package integration

import (
	"fmt"
	"strings"
	"testing"
)

func pruneFiles(names ...string) map[string]string {
	files := map[string]string{}
	for _, name := range names {
		files[name+".yaml"] = manifest(name, "v1")
	}
	return files
}

func exists(e *env, name string) bool {
	_, ok := e.kube.Get("default", name)
	return ok
}

func TestScenario_S_PRUNE_01_RemovedManifestsAreDeletedWhenPruneIsEnabled(t *testing.T) {
	e, p, repo := syncedEnv(t, pruneFiles("keep", "drop"), map[string]any{"prune": true})
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)
	e.kube.Seed("default", "bystander", map[string]string{managedByLabel: "gitopsctl", appLabel: "another-app"}, nil)

	head := commitFiles(t, repo, nil, []string{"drop.yaml"})
	e.waitForApp(p, "", "web", settle, "prune synced", func(st appState) bool { return isSynced(st) && st.LastSynced == head })
	e.waitFor(settle, "pruned resource deleted", func() bool { return !exists(e, "drop") })

	if !exists(e, "keep") || !exists(e, "bystander") {
		t.Fatalf("prune removed the wrong resources: %v", e.kube.Names())
	}
}

func TestScenario_S_PRUNE_02_RemovedManifestsStayWhenPruneIsDisabled(t *testing.T) {
	e, p, repo := syncedEnv(t, pruneFiles("keep", "drop"), nil)
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)

	head := commitFiles(t, repo, nil, []string{"drop.yaml"})
	e.waitForApp(p, "", "web", settle, "second sync", func(st appState) bool { return isSynced(st) && st.LastSynced == head })

	if !exists(e, "drop") || !exists(e, "keep") {
		t.Fatalf("resources were deleted although prune is off: %v", e.kube.Names())
	}
}

func TestScenario_F_PRUNE_01_ResourcesNotOwnedByTheAppSurvivePruning(t *testing.T) {
	e, p, repo := syncedEnv(t, pruneFiles("keep", "drop"), map[string]any{"prune": true})
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)

	e.kube.Seed("default", "drop", map[string]string{"owner": "someone-else"}, map[string]string{"key": "v1"})
	head := commitFiles(t, repo, nil, []string{"drop.yaml"})
	e.waitForApp(p, "", "web", settle, "sync after removal", func(st appState) bool { return isSynced(st) && st.LastSynced == head })

	if !exists(e, "drop") {
		t.Fatal("prune deleted a resource that lost its ownership labels")
	}
}

func TestScenario_F_PRUNE_02_MassDeletionIsRefused(t *testing.T) {
	names := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		names = append(names, fmt.Sprintf("res%02d", i))
	}
	files := pruneFiles(append([]string{"keep"}, names...)...)
	e, p, repo := syncedEnv(t, files, map[string]any{"prune": true})
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)

	removals := make([]string, 0, len(names))
	for _, name := range names {
		removals = append(removals, name+".yaml")
	}
	commitFiles(t, repo, nil, removals)

	st := e.waitForApp(p, "", "web", settle, "prune refusal", func(st appState) bool { return st.Status == "Error" })
	if !strings.Contains(st.Message, "Prune error") {
		t.Fatalf("message = %q", st.Message)
	}
	for _, name := range names {
		if !exists(e, name) {
			t.Fatalf("%s was deleted despite the deletion limit", name)
		}
	}
}

func TestScenario_F_PRUNE_03_NothingIsPrunedWhileTheApplyFails(t *testing.T) {
	e, p, repo := syncedEnv(t, pruneFiles("keep", "drop"), map[string]any{"prune": true})
	e.waitForApp(p, "", "web", settle, "initial sync", isSynced)

	e.kube.failMutationsWhen(func(_, name string) bool { return name == "broken" })
	commitFiles(t, repo, map[string]string{"broken.yaml": manifest("broken", "v1")}, []string{"drop.yaml"})
	e.waitForApp(p, "", "web", settle, "apply failure reported", func(st appState) bool { return st.Status == "Error" })
	if !exists(e, "drop") {
		t.Fatal("prune ran although the apply failed")
	}

	e.kube.failMutationsWhen(nil)
	e.waitForApp(p, "", "web", settle, "recovered", isSynced)
	e.waitFor(settle, "stale resource pruned after recovery", func() bool { return !exists(e, "drop") })
}
