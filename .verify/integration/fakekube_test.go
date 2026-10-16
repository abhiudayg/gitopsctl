package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeKube struct {
	srv *httptest.Server

	mu        sync.Mutex
	objects   map[string]map[string]any
	mutations int
	failWhen  func(method, name string) bool
}

func newFakeKube(t *testing.T) *fakeKube {
	t.Helper()
	k := &fakeKube{objects: map[string]map[string]any{}}
	k.srv = httptest.NewServer(http.HandlerFunc(k.serve))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *fakeKube) URL() string { return k.srv.URL }

func (k *fakeKube) failMutationsWhen(fn func(method, name string) bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.failWhen = fn
}

func (k *fakeKube) Mutations() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.mutations
}

func (k *fakeKube) Get(namespace, name string) (map[string]any, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	obj, ok := k.objects[namespace+"/"+name]
	if !ok {
		return nil, false
	}
	return cloneJSON(obj), true
}

func (k *fakeKube) Seed(namespace, name string, labels map[string]string, data map[string]string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.objects[namespace+"/"+name] = configMap(namespace, name, labels, data)
}

func (k *fakeKube) Names() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	names := make([]string, 0, len(k.objects))
	for key := range k.objects {
		names = append(names, key)
	}
	return names
}

func (k *fakeKube) Labels(namespace, name string) map[string]string {
	obj, ok := k.Get(namespace, name)
	if !ok {
		return nil
	}
	labels := map[string]string{}
	meta, _ := obj["metadata"].(map[string]any)
	raw, _ := meta["labels"].(map[string]any)
	for key, value := range raw {
		labels[key], _ = value.(string)
	}
	return labels
}

func (k *fakeKube) DataValue(namespace, name, key string) string {
	obj, ok := k.Get(namespace, name)
	if !ok {
		return ""
	}
	data, _ := obj["data"].(map[string]any)
	value, _ := data[key].(string)
	return value
}

func configMap(namespace, name string, labels map[string]string, data map[string]string) map[string]any {
	meta := map[string]any{"name": name, "namespace": namespace, "resourceVersion": "1", "uid": "uid-" + name}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	obj := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": meta}
	if len(data) > 0 {
		obj["data"] = data
	}
	return obj
}

func cloneJSON(in map[string]any) map[string]any {
	raw, _ := json.Marshal(in)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeStatus(w http.ResponseWriter, code int, reason, message string) {
	writeJSON(w, code, map[string]any{
		"kind": "Status", "apiVersion": "v1", "metadata": map[string]any{},
		"status": "Failure", "message": message, "reason": reason, "code": code,
	})
}

func (k *fakeKube) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/version":
		writeJSON(w, http.StatusOK, map[string]string{"major": "1", "minor": "30", "gitVersion": "v1.30.0"})
		return
	case "/api":
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
		return
	case "/apis":
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}})
		return
	case "/api/v1":
		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "APIResourceList", "groupVersion": "v1",
			"resources": []map[string]any{{
				"name": "configmaps", "singularName": "configmap", "namespaced": true, "kind": "ConfigMap",
				"verbs": []string{"create", "delete", "get", "list", "update"},
			}},
		})
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "namespaces" || parts[4] != "configmaps" {
		writeStatus(w, http.StatusNotFound, "NotFound", "unsupported path "+r.URL.Path)
		return
	}
	namespace := parts[3]
	name := ""
	if len(parts) > 5 {
		name = parts[5]
	}
	k.handleConfigMap(w, r, namespace, name)
}

func (k *fakeKube) handleConfigMap(w http.ResponseWriter, r *http.Request, namespace, name string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	mutating := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			writeStatus(w, http.StatusBadRequest, "BadRequest", err.Error())
			return
		}
		if r.Method == http.MethodPost {
			meta, _ := body["metadata"].(map[string]any)
			name, _ = meta["name"].(string)
		}
	}

	if mutating {
		if k.failWhen != nil && k.failWhen(r.Method, name) {
			writeStatus(w, http.StatusInternalServerError, "InternalError", "injected failure")
			return
		}
		k.mutations++
	}

	key := namespace + "/" + name
	existing, exists := k.objects[key]
	switch r.Method {
	case http.MethodGet:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", fmt.Sprintf("configmaps %q not found", name))
			return
		}
		writeJSON(w, http.StatusOK, existing)
	case http.MethodPost:
		if exists {
			writeStatus(w, http.StatusConflict, "AlreadyExists", "already exists")
			return
		}
		meta, _ := body["metadata"].(map[string]any)
		meta["namespace"], meta["resourceVersion"], meta["uid"] = namespace, "1", "uid-"+name
		k.objects[key] = body
		writeJSON(w, http.StatusCreated, body)
	case http.MethodPut:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", fmt.Sprintf("configmaps %q not found", name))
			return
		}
		meta, _ := body["metadata"].(map[string]any)
		meta["namespace"], meta["resourceVersion"] = namespace, "2"
		k.objects[key] = body
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", fmt.Sprintf("configmaps %q not found", name))
			return
		}
		delete(k.objects, key)
		writeJSON(w, http.StatusOK, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Success"})
	default:
		writeStatus(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	}
}
