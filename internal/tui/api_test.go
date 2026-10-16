package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func clientWithTransport(fn roundTripFunc) *apiClient {
	client := newAPIClient("http://gitopsctl.test", "")
	client.client.Transport = fn
	client.sseClient.Transport = fn
	return client
}

func TestAPIClientGetApplications(t *testing.T) {
	apps := []AppResponse{{
		Name:        "frontend",
		RepoURL:     "https://example.com/repo.git",
		Branch:      "main",
		Path:        "apps/frontend",
		ClusterName: "prod",
		Interval:    "1m",
		Status:      "Synced",
	}}
	body, err := json.Marshal(apps)
	if err != nil {
		t.Fatal(err)
	}
	client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/applications" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return response(http.StatusOK, string(body)), nil
	})

	got, err := client.getApplications()
	if err != nil {
		t.Fatalf("getApplications() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "frontend" {
		t.Fatalf("unexpected applications %#v", got)
	}
}

func TestAPIClientGetApplicationsErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "server status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusTeapot)
			},
		},
		{
			name: "bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("{"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
				rec := httptestResponseRecorder{header: make(http.Header)}
				tt.handler.ServeHTTP(&rec, r)
				return response(rec.statusCode, rec.body.String()), nil
			})

			if _, err := client.getApplications(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAPIClientGetClusters(t *testing.T) {
	now := time.Date(2026, 5, 9, 10, 30, 0, 0, time.UTC)
	clusters := []ClusterResponse{{
		Name:           "prod",
		KubeconfigPath: "/tmp/kubeconfig",
		RegisteredAt:   now,
		Status:         "Active",
		LastCheckedAt:  now,
	}}
	body, err := json.Marshal(clusters)
	if err != nil {
		t.Fatal(err)
	}
	client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/clusters" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return response(http.StatusOK, string(body)), nil
	})

	got, err := client.getClusters()
	if err != nil {
		t.Fatalf("getClusters() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "prod" {
		t.Fatalf("unexpected clusters %#v", got)
	}
}

func TestAPIClientGetClustersErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "server status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusInternalServerError)
			},
		},
		{
			name: "bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not-json"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
				rec := httptestResponseRecorder{header: make(http.Header)}
				tt.handler.ServeHTTP(&rec, r)
				return response(rec.statusCode, rec.body.String()), nil
			})

			if _, err := client.getClusters(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAPIClientActionMethods(t *testing.T) {
	var seen []string
	client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		return response(http.StatusNoContent, ""), nil
	})

	if err := client.syncApp("frontend"); err != nil {
		t.Fatalf("syncApp() error = %v", err)
	}
	if err := client.unregisterApp("frontend"); err != nil {
		t.Fatalf("unregisterApp() error = %v", err)
	}
	if err := client.checkCluster("prod"); err != nil {
		t.Fatalf("checkCluster() error = %v", err)
	}
	if err := client.unregisterCluster("prod"); err != nil {
		t.Fatalf("unregisterCluster() error = %v", err)
	}

	want := []string{
		"POST /api/v1/applications/frontend/sync",
		"DELETE /api/v1/applications/frontend",
		"POST /api/v1/clusters/prod/check",
		"DELETE /api/v1/clusters/prod",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected requests\nwant %v\ngot  %v", want, seen)
	}
}

func TestAPIClientActionMethodsReturnTransportErrors(t *testing.T) {
	client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("transport down")
	})
	for name, call := range map[string]func() error{
		"syncApp":           func() error { return client.syncApp("frontend") },
		"unregisterApp":     func() error { return client.unregisterApp("frontend") },
		"checkCluster":      func() error { return client.checkCluster("prod") },
		"unregisterCluster": func() error { return client.unregisterCluster("prod") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("expected transport error")
			}
		})
	}
}

func TestListenForEvents(t *testing.T) {
	client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/events" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return response(http.StatusOK, ": keepalive\ndata: {\"type\":\"application.synced\"}\n"), nil
	})

	msg := client.listenForEvents(context.Background())()
	if _, ok := msg.(eventReceivedMsg); !ok {
		t.Fatalf("expected eventReceivedMsg, got %#v", msg)
	}
}

func TestListenForEventsErrorsAndEOF(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("transport down")
		})
		msg := client.listenForEvents(context.Background())()
		if _, ok := msg.(errorMsg); !ok {
			t.Fatalf("expected errorMsg, got %#v", msg)
		}
	})

	t.Run("end of stream", func(t *testing.T) {
		client := clientWithTransport(func(r *http.Request) (*http.Response, error) {
			return response(http.StatusOK, ": keepalive\n"), nil
		})
		msg := client.listenForEvents(context.Background())()
		if _, ok := msg.(sseDisconnectedMsg); !ok {
			t.Fatalf("expected sseDisconnectedMsg, got %#v", msg)
		}
	})
}

type httptestResponseRecorder struct {
	header     http.Header
	body       strings.Builder
	statusCode int
}

func (r *httptestResponseRecorder) Header() http.Header {
	return r.header
}

func (r *httptestResponseRecorder) Write(p []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *httptestResponseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func (r *httptestResponseRecorder) Flush() {}

func TestAPIClientSendsBearerToken(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	for _, token := range []string{"", "tok"} {
		got = nil
		client := newAPIClient(srv.URL, token)
		if _, err := client.getApplications(); err != nil {
			t.Fatal(err)
		}
		if _, err := client.sseClient.Get(srv.URL); err != nil {
			t.Fatal(err)
		}
		want := ""
		if token != "" {
			want = "Bearer " + token
		}
		if len(got) != 2 || got[0] != want || got[1] != want {
			t.Fatalf("token %q: got headers %v, want %q", token, got, want)
		}
	}
}
