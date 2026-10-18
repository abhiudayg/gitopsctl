package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

type resourceDef struct {
	group, version, plural, kind string
	namespaced                   bool
}

var resourceDefs = []resourceDef{
	{"", "v1", "configmaps", "ConfigMap", true},
	{"", "v1", "namespaces", "Namespace", false},
	{"", "v1", "resourcequotas", "ResourceQuota", true},
	{"apps", "v1", "deployments", "Deployment", true},
	{"rbac.authorization.k8s.io", "v1", "clusterrolebindings", "ClusterRoleBinding", false},
}

type fakeKube struct {
	srv *httptest.Server

	mu        sync.Mutex
	objects   map[string]map[string]any
	mutations int
	failWhen  func(method, name string) bool
	nextUID   int
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

func objectKey(plural, namespace, name string) string { return plural + "|" + namespace + "|" + name }

func (k *fakeKube) Get(namespace, name string) (map[string]any, bool) {
	return k.getObject("configmaps", namespace, name)
}

func (k *fakeKube) getObject(plural, namespace, name string) (map[string]any, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	obj, ok := k.objects[objectKey(plural, namespace, name)]
	if !ok {
		return nil, false
	}
	return cloneJSON(obj), true
}

func (k *fakeKube) Seed(namespace, name string, labels map[string]string, data map[string]string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.objects[objectKey("configmaps", namespace, name)] = configMap(namespace, name, labels, data)
}

func (k *fakeKube) SeedNamespace(name string, labels map[string]string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	meta := map[string]any{"name": name, "resourceVersion": "1", "uid": "uid-ns-" + name}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	k.objects[objectKey("namespaces", "", name)] = map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": meta}
}

func (k *fakeKube) NamespaceExists(name string) bool {
	_, ok := k.getObject("namespaces", "", name)
	return ok
}

func (k *fakeKube) NamespaceLabels(name string) map[string]string {
	obj, ok := k.getObject("namespaces", "", name)
	if !ok {
		return nil
	}
	return labelsOf(obj)
}

func (k *fakeKube) NamespaceNames() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var names []string
	for key := range k.objects {
		if parts := strings.SplitN(key, "|", 3); parts[0] == "namespaces" {
			names = append(names, parts[2])
		}
	}
	sort.Strings(names)
	return names
}

func (k *fakeKube) Names() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var names []string
	for key := range k.objects {
		if parts := strings.SplitN(key, "|", 3); parts[0] == "configmaps" {
			names = append(names, parts[1]+"/"+parts[2])
		}
	}
	return names
}

func (k *fakeKube) CountIn(namespace string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for key := range k.objects {
		if parts := strings.SplitN(key, "|", 3); parts[1] == namespace && parts[0] != "namespaces" {
			n++
		}
	}
	return n
}

func (k *fakeKube) Labels(namespace, name string) map[string]string {
	obj, ok := k.Get(namespace, name)
	if !ok {
		return nil
	}
	return labelsOf(obj)
}

func labelsOf(obj map[string]any) map[string]string {
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

func resourceList(group, version string) map[string]any {
	groupVersion := version
	if group != "" {
		groupVersion = group + "/" + version
	}
	var resources []map[string]any
	for _, def := range resourceDefs {
		if def.group == group && def.version == version {
			resources = append(resources, map[string]any{
				"name": def.plural, "singularName": strings.ToLower(def.kind), "namespaced": def.namespaced, "kind": def.kind,
				"verbs": []string{"create", "delete", "get", "list", "update"},
			})
		}
	}
	return map[string]any{"kind": "APIResourceList", "groupVersion": groupVersion, "resources": resources}
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
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{
			map[string]any{"name": "apps", "versions": []any{map[string]string{"groupVersion": "apps/v1", "version": "v1"}}, "preferredVersion": map[string]string{"groupVersion": "apps/v1", "version": "v1"}},
			map[string]any{"name": "rbac.authorization.k8s.io", "versions": []any{map[string]string{"groupVersion": "rbac.authorization.k8s.io/v1", "version": "v1"}}, "preferredVersion": map[string]string{"groupVersion": "rbac.authorization.k8s.io/v1", "version": "v1"}},
		}})
		return
	case "/api/v1":
		writeJSON(w, http.StatusOK, resourceList("", "v1"))
		return
	case "/apis/apps/v1":
		writeJSON(w, http.StatusOK, resourceList("apps", "v1"))
		return
	case "/apis/rbac.authorization.k8s.io/v1":
		writeJSON(w, http.StatusOK, resourceList("rbac.authorization.k8s.io", "v1"))
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	group, version, rest := "", "", []string(nil)
	switch {
	case len(parts) >= 3 && parts[0] == "api" && parts[1] == "v1":
		version, rest = "v1", parts[2:]
	case len(parts) >= 4 && parts[0] == "apis":
		group, version, rest = parts[1], parts[2], parts[3:]
	default:
		writeStatus(w, http.StatusNotFound, "NotFound", "unsupported path "+r.URL.Path)
		return
	}

	namespace := ""
	if len(rest) >= 3 && rest[0] == "namespaces" {
		namespace, rest = rest[1], rest[2:]
	}
	plural, name := rest[0], ""
	if len(rest) > 1 {
		name = rest[1]
	}
	for _, def := range resourceDefs {
		if def.group == group && def.version == version && def.plural == plural && def.namespaced == (namespace != "") {
			k.handle(w, r, def, namespace, name)
			return
		}
	}
	writeStatus(w, http.StatusNotFound, "NotFound", "unsupported resource "+r.URL.Path)
}

func selectorMatches(selector string, labels map[string]string) bool {
	if selector == "" {
		return true
	}
	for _, term := range strings.Split(selector, ",") {
		key, want, hasValue := strings.Cut(term, "=")
		key = strings.TrimSpace(key)
		value, present := labels[key]
		if !present || (hasValue && value != strings.TrimPrefix(want, "=")) {
			return false
		}
	}
	return true
}

func (k *fakeKube) handle(w http.ResponseWriter, r *http.Request, def resourceDef, namespace, name string) {
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

	if r.Method == http.MethodGet && name == "" {
		k.list(w, r, def, namespace)
		return
	}

	if mutating {
		if k.failWhen != nil && k.failWhen(r.Method, name) {
			writeStatus(w, http.StatusInternalServerError, "InternalError", "injected failure")
			return
		}
		k.mutations++
	}

	key := objectKey(def.plural, namespace, name)
	existing, exists := k.objects[key]
	notFound := fmt.Sprintf("%s %q not found", def.plural, name)
	switch r.Method {
	case http.MethodGet:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", notFound)
			return
		}
		writeJSON(w, http.StatusOK, existing)
	case http.MethodPost:
		if exists {
			writeStatus(w, http.StatusConflict, "AlreadyExists", "already exists")
			return
		}
		meta, _ := body["metadata"].(map[string]any)
		k.nextUID++
		meta["resourceVersion"], meta["uid"] = "1", fmt.Sprintf("uid-%s-%d", name, k.nextUID)
		if def.namespaced {
			meta["namespace"] = namespace
		}
		k.objects[key] = body
		writeJSON(w, http.StatusCreated, body)
	case http.MethodPut:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", notFound)
			return
		}
		meta, _ := body["metadata"].(map[string]any)
		meta["resourceVersion"] = "2"
		if def.namespaced {
			meta["namespace"] = namespace
		}
		k.objects[key] = body
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		if !exists {
			writeStatus(w, http.StatusNotFound, "NotFound", notFound)
			return
		}
		delete(k.objects, key)
		if def.plural == "namespaces" {
			for other := range k.objects {
				if parts := strings.SplitN(other, "|", 3); parts[1] == name {
					delete(k.objects, other)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Success"})
	default:
		writeStatus(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	}
}

func (k *fakeKube) list(w http.ResponseWriter, r *http.Request, def resourceDef, namespace string) {
	selector := r.URL.Query().Get("labelSelector")
	items := []any{}
	for key, obj := range k.objects {
		parts := strings.SplitN(key, "|", 3)
		if parts[0] != def.plural || (def.namespaced && parts[1] != namespace) || !selectorMatches(selector, labelsOf(cloneJSON(obj))) {
			continue
		}
		items = append(items, obj)
	}
	apiVersion := def.version
	if def.group != "" {
		apiVersion = def.group + "/" + def.version
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": def.kind + "List", "apiVersion": apiVersion, "metadata": map[string]any{}, "items": items})
}
