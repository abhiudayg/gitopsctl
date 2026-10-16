package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitopsctl-verify-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "gitopsctl")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type env struct {
	t    *testing.T
	root string
	kube *fakeKube
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, root: root, kube: newFakeKube(t)}
}

func (e *env) configPath(name string) string { return filepath.Join(e.root, "configs", name) }

func (e *env) run(args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = e.root
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out.String(), exitErr.ExitCode()
	}
	e.t.Fatalf("run %v: %v", args, err)
	return "", -1
}

type proc struct {
	cmd  *exec.Cmd
	out  *syncBuffer
	done chan struct{}
	port int
}

func (p *proc) Output() string { return p.out.String() }

func (p *proc) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *proc) ExitCode() int {
	<-p.done
	return p.cmd.ProcessState.ExitCode()
}

func (p *proc) WaitExit(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *proc) Stop() {
	if !p.Alive() {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func (e *env) startNoWait(args ...string) *proc {
	e.t.Helper()
	port := freePort(e.t)
	return e.launch(port, append([]string{"start", "--api-address", fmt.Sprintf("127.0.0.1:%d", port)}, args...))
}

func (e *env) launch(port int, full []string) *proc {
	e.t.Helper()
	cmd := exec.Command(binaryPath, full...)
	cmd.Dir = e.root
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	p := &proc{cmd: cmd, out: out, done: make(chan struct{}), port: port}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	e.t.Cleanup(p.Stop)
	return p
}

func (e *env) start(args ...string) *proc {
	e.t.Helper()
	p := e.startNoWait(args...)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !p.Alive() {
			e.t.Fatalf("controller exited early:\n%s", p.Output())
		}
		resp, err := http.Get(p.url("/health"))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return p
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.t.Fatalf("controller did not become healthy:\n%s", p.Output())
	return nil
}

func (p *proc) url(path string) string { return fmt.Sprintf("http://127.0.0.1:%d%s", p.port, path) }

func (p *proc) api(method, path, token string, body any) (int, []byte) {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, p.url(path), reader)
	if err != nil {
		panic(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func (p *proc) header(method, path, token, name string) string {
	req, _ := http.NewRequest(method, p.url(path), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.Header.Get(name)
}

type appState struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	LastSynced   string `json:"last_synced_git_hash"`
	LatestHash   string `json:"latest_git_hash"`
	Failures     int    `json:"consecutive_failures"`
	SyncPolicy   string `json:"sync_policy"`
	ApprovedHash string `json:"approved_git_hash"`
	Prune        bool   `json:"prune"`
}

func (p *proc) appState(token, name string) (appState, bool) {
	status, body := p.api(http.MethodGet, "/api/v1/applications/"+name, token, nil)
	if status != http.StatusOK {
		return appState{}, false
	}
	var st appState
	if err := json.Unmarshal(body, &st); err != nil {
		return appState{}, false
	}
	return st, true
}

func (e *env) waitFor(timeout time.Duration, desc string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.t.Fatalf("timed out after %s waiting for: %s", timeout, desc)
}

func (e *env) waitForApp(p *proc, token, name string, timeout time.Duration, desc string, pred func(appState) bool) appState {
	e.t.Helper()
	var last appState
	e.waitFor(timeout, desc+fmt.Sprintf(" (app %s)", name), func() bool {
		st, ok := p.appState(token, name)
		last = st
		return ok && pred(st)
	})
	return last
}

func (e *env) writeKubeconfig(path, server string) {
	e.t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
  name: fake
contexts:
- context:
    cluster: fake
    user: fake
  name: fake
current-context: fake
users:
- name: fake
  user: {}
`, server)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) writeJSON(path string, v any) {
	e.t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) useFakeCluster(name string, allowedNamespaces ...string) {
	e.t.Helper()
	kubeconfig := filepath.Join(e.root, "kubeconfig.yaml")
	e.writeKubeconfig(kubeconfig, e.kube.URL())
	cluster := map[string]any{"name": name, "kubeconfig_path": kubeconfig, "registered_at": time.Now().UTC().Format(time.RFC3339)}
	if len(allowedNamespaces) > 0 {
		cluster["allowed_namespaces"] = allowedNamespaces
	}
	e.writeJSON(e.configPath("clusters.json"), []any{cluster})
}

func (e *env) writeApps(apps ...map[string]any) {
	e.t.Helper()
	list := make([]any, 0, len(apps))
	for _, app := range apps {
		list = append(list, app)
	}
	e.writeJSON(e.configPath("applications.json"), list)
}

func appSpec(name, repo string, extra map[string]any) map[string]any {
	app := map[string]any{
		"name": name, "repo_url": repo, "branch": "master", "path": ".",
		"cluster_name": "c1", "interval": "1s", "status": "Pending",
	}
	for key, value := range extra {
		app[key] = value
	}
	return app
}

func manifest(name, value string) string {
	return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\ndata:\n  key: %s\n", name, value)
}

func manifestIn(namespace, name, value string) string {
	return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  key: %s\n", name, namespace, value)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=verify", "-c", "user.email=verify@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "master")
	commitFiles(t, dir, files, nil)
	return dir
}

func commitFiles(t *testing.T, repo string, write map[string]string, remove []string) string {
	t.Helper()
	for name, content := range write {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range remove {
		if err := os.Remove(filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "change", "--allow-empty")
	return git(t, repo, "rev-parse", "HEAD")
}

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	appLabel       = "gitopsctl.io/app"
	clusterLabel   = "gitopsctl.io/cluster"
)
