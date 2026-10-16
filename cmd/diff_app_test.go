package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestDiffAppCommand(t *testing.T) {
	origURL, origToken, origName := apiBaseURL, apiToken, diffAppName
	t.Cleanup(func() { apiBaseURL, apiToken, diffAppName = origURL, origToken, origName })

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"app":"web","commit":"abc123","items":[
			{"action":"create","kind":"ConfigMap","namespace":"default","name":"new"},
			{"action":"update","kind":"Deployment","namespace":"default","name":"api"},
			{"action":"prune","kind":"Service","namespace":"default","name":"old"},
			{"action":"unchanged","kind":"ConfigMap","namespace":"default","name":"same"}]}`))
	}))
	defer srv.Close()
	apiBaseURL, apiToken, diffAppName = srv.URL, "tok", "web"

	var runErr error
	out := captureStdout(t, func() { runErr = diffAppCmd.RunE(diffAppCmd, nil) })

	if runErr != nil {
		t.Fatal(runErr)
	}
	if gotPath != "/api/v1/applications/web/plan" || gotAuth != "Bearer tok" {
		t.Fatalf("unexpected request: %s auth=%q", gotPath, gotAuth)
	}
	for _, want := range []string{"Plan for web at abc123", "+ create", "~ update", "- prune", "= unchanged", "1 to create, 1 to update, 1 to prune, 1 unchanged"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDiffAppCommand_Errors(t *testing.T) {
	origURL, origName := apiBaseURL, diffAppName
	t.Cleanup(func() { apiBaseURL, diffAppName = origURL, origName })
	diffAppName = "web"

	for name, handler := range map[string]http.HandlerFunc{
		"http error":   func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusNotFound) },
		"invalid json": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			apiBaseURL = srv.URL
			if err := diffAppCmd.RunE(diffAppCmd, nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
