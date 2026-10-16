package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/policy"
	"aeswibon.com/github/gitopsctl/internal/session"
	"aeswibon.com/github/gitopsctl/internal/session/sessiontest"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const config = `
policy:
  allowed_kinds: [ConfigMap]
  allowed_namespaces: ["agent-*"]
agents:
  - id: alpha
    cluster: dev
    repos: ["https://git.example/acme/"]
    max_sessions: 1
    max_ttl: 1h
  - id: beta
    cluster: dev
    repos: ["https://git.example/acme/"]
    max_sessions: 2
`

type fixture struct {
	e       *echo.Echo
	cluster *sessiontest.Cluster
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cfg, err := session.ParseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "cm.yaml"), []byte("kind: ConfigMap\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cluster := sessiontest.NewCluster()
	mgr, err := session.NewManager(zap.NewNop(), cfg, func(string) (session.Cluster, error) { return cluster, nil }, filepath.Join(t.TempDir(), "s.json"),
		session.WithFetcher(func(context.Context, string, string) (string, string, func(), error) {
			return repo, "deadbeef", func() {}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	g := e.Group("/api/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(AgentKey, c.Request().Header.Get("X-Test-Agent"))
			return next(c)
		}
	})
	RegisterRoutes(g, NewHandler(zap.NewNop(), mgr))
	return &fixture{e: e, cluster: cluster}
}

func (f *fixture) do(method, path, agent, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Test-Agent", agent)
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func createSession(t *testing.T, f *fixture, agent string) session.Session {
	t.Helper()
	rec := f.do(http.MethodPost, "/api/v1/sessions", agent, `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	return decode[session.Session](t, rec)
}

const deployBody = `{"repo":"https://git.example/acme/app","ref":"main","path":"."}`

func TestLifecycle(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	if s.Agent != "alpha" || !f.cluster.Has(s.Namespace) {
		t.Fatalf("unexpected session %+v", s)
	}

	if rec := f.do(http.MethodGet, "/api/v1/sessions/"+s.ID, "alpha", ""); rec.Code != http.StatusOK {
		t.Fatalf("get: %d", rec.Code)
	}
	rec := f.do(http.MethodPost, "/api/v1/sessions/"+s.ID+"/deploy", "alpha", deployBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body)
	}
	deployed := decode[session.Session](t, rec)
	if deployed.Commit != "deadbeef" || len(deployed.Resources) != 1 || deployed.Resources[0].Kind != "ConfigMap" {
		t.Fatalf("unexpected deploy result %+v", deployed)
	}
	if list := decode[[]session.Session](t, f.do(http.MethodGet, "/api/v1/sessions", "alpha", "")); len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if rec := f.do(http.MethodDelete, "/api/v1/sessions/"+s.ID, "alpha", ""); rec.Code != http.StatusOK {
		t.Fatalf("teardown: %d", rec.Code)
	}
	if f.cluster.Has(s.Namespace) {
		t.Fatal("namespace should be removed")
	}
	if rec := f.do(http.MethodGet, "/api/v1/sessions/"+s.ID, "alpha", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get after teardown: %d", rec.Code)
	}
}

func TestListIsAlwaysAnArray(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, "/api/v1/sessions", "alpha", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestAgentCannotTouchAnotherAgentsSession(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/sessions/" + s.ID, ""},
		{http.MethodPost, "/api/v1/sessions/" + s.ID + "/deploy", deployBody},
		{http.MethodDelete, "/api/v1/sessions/" + s.ID, ""},
	} {
		if rec := f.do(tc.method, tc.path, "beta", tc.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: got %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	if !f.cluster.Has(s.Namespace) {
		t.Fatal("the namespace must be untouched")
	}
	if list := decode[[]session.Session](t, f.do(http.MethodGet, "/api/v1/sessions", "beta", "")); len(list) != 0 {
		t.Fatalf("beta sees %+v", list)
	}
}

func TestCreateValidation(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name  string
		agent string
		body  string
		want  int
	}{
		{"admin needs an agent", "", `{}`, http.StatusBadRequest},
		{"admin creates for agent", "", `{"agent":"beta"}`, http.StatusCreated},
		{"agent cannot impersonate", "alpha", `{"agent":"beta"}`, http.StatusForbidden},
		{"unknown agent", "", `{"agent":"ghost"}`, http.StatusForbidden},
		{"bad ttl", "alpha", `{"ttl":"soon"}`, http.StatusBadRequest},
		{"ttl above max", "alpha", `{"ttl":"5h"}`, http.StatusBadRequest},
		{"malformed body", "alpha", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := f.do(http.MethodPost, "/api/v1/sessions", tc.agent, tc.body); rec.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestSessionLimitReturns429(t *testing.T) {
	f := newFixture(t)
	createSession(t, f, "alpha")
	if rec := f.do(http.MethodPost, "/api/v1/sessions", "alpha", `{}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestDeployErrorMapping(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	path := "/api/v1/sessions/" + s.ID + "/deploy"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"repo not allowed", `{"repo":"https://evil.example/x","ref":"main","path":"."}`, http.StatusForbidden},
		{"path traversal", `{"repo":"https://git.example/acme/app","ref":"main","path":"../.."}`, http.StatusBadRequest},
		{"bad ref", `{"repo":"https://git.example/acme/app","ref":"--x","path":"."}`, http.StatusBadRequest},
		{"malformed body", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := f.do(http.MethodPost, path, "alpha", tc.body); rec.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestDeployPolicyViolationsAreListed(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	f.cluster.ApplyErr = errors.Join(
		policy.Violation{Rule: "privileged", Object: "Deployment a/b", Message: "no"},
		policy.Violation{Rule: "host-path", Object: "Deployment a/b", Message: "no"},
	)
	rec := f.do(http.MethodPost, "/api/v1/sessions/"+s.ID+"/deploy", "alpha", deployBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	body := decode[PolicyErrorResponse](t, rec)
	if len(body.Violations) != 2 || body.Violations[0].Rule != "privileged" || body.Violations[1].Rule != "host-path" {
		t.Fatalf("unexpected violations %+v", body)
	}
}

func TestDeployUnexpectedFailureHidesDetails(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	f.cluster.ApplyErr = errors.New("dial tcp 10.0.0.5:6443: secret-internal-detail")
	rec := f.do(http.MethodPost, "/api/v1/sessions/"+s.ID+"/deploy", "alpha", deployBody)
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "secret-internal-detail") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestDeployAfterTeardownConflict(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	f.do(http.MethodDelete, "/api/v1/sessions/"+s.ID, "alpha", "")
	if rec := f.do(http.MethodPost, "/api/v1/sessions/"+s.ID+"/deploy", "alpha", deployBody); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestIncompleteTeardownIsAcceptedAndRetriedLater(t *testing.T) {
	f := newFixture(t)
	s := createSession(t, f, "alpha")
	f.cluster.DeleteErr = errors.New("api down")

	rec := f.do(http.MethodDelete, "/api/v1/sessions/"+s.ID, "alpha", "")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d", rec.Code)
	}
	if got := decode[session.Session](t, rec); got.State != session.StateTearingDown {
		t.Fatalf("state %s", got.State)
	}
	if rec := f.do(http.MethodDelete, "/api/v1/sessions/unknown", "alpha", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", rec.Code)
	}
}
