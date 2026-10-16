package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostAccepted_SendsBearerTokenWhenConfigured(t *testing.T) {
	origURL, origToken := apiBaseURL, apiToken
	t.Cleanup(func() { apiBaseURL, apiToken = origURL, origToken })

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	apiBaseURL = srv.URL

	apiToken = ""
	if _, err := postAccepted("/x", nil); err != nil || got != "" {
		t.Fatalf("expected no auth header, got %q (%v)", got, err)
	}
	apiToken = "secret"
	if _, err := postAccepted("/x", nil); err != nil || got != "Bearer secret" {
		t.Fatalf("expected bearer header, got %q (%v)", got, err)
	}
}

func TestPostAccepted(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		payload     any
		wantBody    string
		wantContent string
		wantErr     bool
	}{
		{name: "accepted without payload", status: http.StatusAccepted, wantBody: "ok"},
		{name: "accepted with json payload", status: http.StatusAccepted, payload: map[string]string{"k": "v"}, wantBody: `{"k":"v"}`, wantContent: "application/json"},
		{name: "non-accepted status is an error", status: http.StatusNotFound, wantErr: true},
		{name: "unmarshalable payload", status: http.StatusAccepted, payload: make(chan int), wantErr: true},
	}

	orig := apiBaseURL
	t.Cleanup(func() { apiBaseURL = orig })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody, gotContent, gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotBody, gotContent, gotMethod, gotPath = string(b), r.Header.Get("Content-Type"), r.Method, r.URL.Path
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("ok\n"))
			}))
			defer srv.Close()
			apiBaseURL = srv.URL + "/"

			out, err := postAccepted("/x/y", tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != "ok" || gotMethod != http.MethodPost || gotPath != "/x/y" {
				t.Fatalf("out=%q method=%s path=%s", out, gotMethod, gotPath)
			}
			if tc.payload != nil && (gotBody != tc.wantBody || gotContent != tc.wantContent) {
				t.Fatalf("body=%q content-type=%q", gotBody, gotContent)
			}
			if tc.payload == nil && (gotBody != "" || gotContent != "") {
				t.Fatalf("expected empty body/content-type, got %q %q", gotBody, gotContent)
			}
		})
	}
}
