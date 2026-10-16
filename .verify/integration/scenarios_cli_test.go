package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func readList(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("%s is not valid JSON: %v\n%s", path, err, raw)
	}
	return list
}

func TestScenario_S_BUILD_01_BinaryBuildsAndExposesCoreCommands(t *testing.T) {
	e := newEnv(t)
	out, code := e.run("--help")
	if code != 0 {
		t.Fatalf("--help exited %d:\n%s", code, out)
	}
	for _, cmd := range []string{"start", "register-apps", "register-cluster", "sync-app", "approve-app", "diff-app", "check-cluster", "dashboard", "list-apps", "unregister"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("--help does not list %q", cmd)
		}
	}
	startHelp, _ := e.run("start", "--help")
	for _, flag := range []string{"--api-address", "--api-token", "--api-token-file", "--api-cors-origin"} {
		if !strings.Contains(startHelp, flag) {
			t.Errorf("start --help does not list %s", flag)
		}
	}
}

func TestScenario_S_CLI_01_RegistrationPersistsStateAtomically(t *testing.T) {
	e := newEnv(t)
	kubeconfig := filepath.Join(e.root, "kubeconfig.yaml")
	e.writeKubeconfig(kubeconfig, e.kube.URL())

	if out, code := e.run("register-cluster", "-n", "c1", "-k", kubeconfig, "--allowed-namespaces", "demo"); code != 0 {
		t.Fatalf("register-cluster failed (%d):\n%s", code, out)
	}
	if out, code := e.run("register-apps", "-n", "web", "--repo", "https://example.com/org/repo.git", "--path", "k8s", "--cluster", "c1", "--interval", "30s", "--prune"); code != 0 {
		t.Fatalf("register-apps failed (%d):\n%s", code, out)
	}

	clusters := readList(t, e.configPath("clusters.json"))
	apps := readList(t, e.configPath("applications.json"))
	if len(clusters) != 1 || clusters[0]["name"] != "c1" || len(apps) != 1 || apps[0]["name"] != "web" || apps[0]["prune"] != true {
		t.Fatalf("unexpected state: clusters=%v apps=%v", clusters, apps)
	}
	if out, _ := e.run("list-apps"); !strings.Contains(out, "web") || !strings.Contains(out, "example.com/org/rep") {
		t.Errorf("list-apps does not show the application:\n%s", out)
	}

	entries, _ := os.ReadDir(filepath.Join(e.root, "configs"))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestScenario_F_CLI_01_InvalidApplicationRegistrationsAreRejectedWithoutSideEffects(t *testing.T) {
	e := newEnv(t)
	kubeconfig := filepath.Join(e.root, "kubeconfig.yaml")
	e.writeKubeconfig(kubeconfig, e.kube.URL())
	if _, code := e.run("register-cluster", "-n", "c1", "-k", kubeconfig); code != 0 {
		t.Fatal("setup: register-cluster failed")
	}

	good := []string{"--repo", "https://example.com/org/repo.git", "--path", "k8s", "--cluster", "c1"}
	cases := map[string][]string{
		"invalid repo url":     {"-n", "bad", "--repo", "not a url", "--path", "k8s", "--cluster", "c1"},
		"interval too short":   append([]string{"-n", "bad", "--interval", "5s"}, good...),
		"invalid sync policy":  append([]string{"-n", "bad", "--sync-policy", "sometimes"}, good...),
		"unknown cluster":      {"-n", "bad", "--repo", "https://example.com/org/repo.git", "--path", "k8s", "--cluster", "nope"},
		"missing required arg": {"-n", "bad"},
	}
	for name, args := range cases {
		if out, code := e.run(append([]string{"register-apps"}, args...)...); code == 0 {
			t.Errorf("%s: expected failure, got success:\n%s", name, out)
		}
	}
	if apps := readList(t, e.configPath("applications.json")); len(apps) != 0 {
		t.Fatalf("rejected registrations persisted state: %v", apps)
	}
}

func TestScenario_F_CLI_02_InvalidClusterRegistrationsAreRejectedWithoutSideEffects(t *testing.T) {
	e := newEnv(t)
	notKubeconfig := filepath.Join(e.root, "not-a-kubeconfig")
	if err := os.WriteFile(notKubeconfig, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"missing kubeconfig":    {"-n", "c2", "-k", "/nonexistent/kubeconfig"},
		"invalid kubeconfig":    {"-n", "c2", "-k", notKubeconfig},
		"invalid cluster name":  {"-n", "Bad Name!", "-k", notKubeconfig},
		"directory as kubeconf": {"-n", "c2", "-k", e.root},
	}
	for name, args := range cases {
		if out, code := e.run(append([]string{"register-cluster"}, args...)...); code == 0 {
			t.Errorf("%s: expected failure, got success:\n%s", name, out)
		}
	}
	if clusters := readList(t, e.configPath("clusters.json")); len(clusters) != 0 {
		t.Fatalf("rejected registrations persisted state: %v", clusters)
	}
}

func TestScenario_S_DOC_01_ExamplesLoadAndFlagsAreDocumented(t *testing.T) {
	e := newEnv(t)
	for src, dst := range map[string]string{"examples/configs/apps.json": "applications.json", "examples/configs/clusters.json": "clusters.json"} {
		if err := os.WriteFile(e.configPath(dst), []byte(repoFile(t, src)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, code := e.run("list-apps")
	if code != 0 || !strings.Contains(out, "nginx-demo") || !strings.Contains(out, "github.com/aeswibon") || !strings.Contains(out, "local-dev") {
		t.Fatalf("example application config does not load into usable state (exit %d):\n%s", code, out)
	}
	out, code = e.run("list-clusters")
	if code != 0 || !strings.Contains(out, "local-dev") {
		t.Fatalf("example cluster config does not load (exit %d):\n%s", code, out)
	}

	docs := repoFile(t, "docs/cli-reference.md") + repoFile(t, "docs/configuration.md")
	help, _ := e.run("start", "--help")
	for _, flag := range regexp.MustCompile(`--[a-z][a-z-]+`).FindAllString(help, -1) {
		if flag == "--help" {
			continue
		}
		if !strings.Contains(docs, flag) {
			t.Errorf("flag %s of `start` is not documented in docs/", flag)
		}
	}
}

func TestScenario_S_REL_01_ReleaseSupplyChainIsHardened(t *testing.T) {
	if !strings.Contains(repoFile(t, "LICENSE"), "Apache License") {
		t.Error("LICENSE is missing or not Apache-2.0")
	}

	goreleaser := repoFile(t, ".goreleaser.yaml")
	for _, key := range []string{"checksum:", "sboms:", "signs:", "docker_signs:"} {
		if !strings.Contains(goreleaser, key) {
			t.Errorf(".goreleaser.yaml lacks %s", key)
		}
	}
	if !regexp.MustCompile(`(?m)^FROM \S+@sha256:[0-9a-f]{64}`).MatchString(repoFile(t, "Dockerfile")) {
		t.Error("Dockerfile base image is not pinned by digest")
	}

	goVersion := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(repoFile(t, "go.mod"))
	workflows, _ := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if goVersion == nil || len(workflows) == 0 {
		t.Fatal("cannot find go.mod version or workflows")
	}
	pinned := regexp.MustCompile(`uses:\s+\S+@[0-9a-f]{40}\s*$`)
	for _, workflow := range workflows {
		raw, _ := os.ReadFile(workflow)
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "uses:") && !pinned.MatchString(trimmed) {
				t.Errorf("%s: action not pinned by SHA: %s", filepath.Base(workflow), trimmed)
			}
			if strings.Contains(trimmed, "go-version:") && !strings.Contains(trimmed, `"`+goVersion[1]+`"`) {
				t.Errorf("%s: %s does not match go.mod (%s)", filepath.Base(workflow), trimmed, goVersion[1])
			}
			if strings.Contains(trimmed, "@latest") || trimmed == "version: latest" {
				t.Errorf("%s: unpinned tool version: %s", filepath.Base(workflow), trimmed)
			}
		}
	}
}
