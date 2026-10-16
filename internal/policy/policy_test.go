package policy

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const validPolicy = `
allowed_kinds: [ConfigMap, Service, apps/Deployment, batch/CronJob, Pod, apps/ReplicaSet]
allowed_namespaces: ["agent-*"]
denied_namespaces: ["agent-admin"]
allowed_registries: [ghcr.io/acme, docker.io]
max_objects: 5
max_replicas: 3
`

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := Parse([]byte(validPolicy))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func object(t *testing.T, manifest string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}
	data, err := yaml.YAMLToJSON([]byte(manifest))
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if err := obj.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return obj
}

func deployment(podSpec string) string {
	return `
apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: agent-1}
spec:
  replicas: 2
  template:
    spec:
` + podSpec
}

const goodPodSpec = `
      containers:
      - name: app
        image: ghcr.io/acme/app:1
        securityContext: {runAsNonRoot: true}
        resources: {limits: {cpu: 100m, memory: 64Mi}}
`

func rules(violations []Violation) string {
	var names []string
	for _, v := range violations {
		names = append(names, v.Rule)
	}
	return strings.Join(names, ",")
}

func TestCheck_AllowsCompliantDeployment(t *testing.T) {
	items := []Item{{Object: object(t, deployment(goodPodSpec)), Namespaced: true}}
	if v := testPolicy(t).Check(items, Scope{}); len(v) != 0 {
		t.Fatalf("expected no violations, got %v", v)
	}
}

func TestCheck_Violations(t *testing.T) {
	cases := []struct {
		name       string
		manifest   string
		namespaced bool
		scope      Scope
		wantRule   string
	}{
		{"kind not listed", "apiVersion: v1\nkind: Secret\nmetadata: {name: s, namespace: agent-1}", true, Scope{}, "kind"},
		{"same kind other group", "apiVersion: example.com/v1\nkind: Service\nmetadata: {name: s, namespace: agent-1}", true, Scope{}, "kind"},
		{"rbac never allowed", "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata: {name: b}", false, Scope{}, "kind"},
		{"namespace kind", "apiVersion: v1\nkind: Namespace\nmetadata: {name: agent-x}", false, Scope{}, "kind"},
		{"cluster scoped", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c}", false, Scope{}, "cluster-scoped"},
		{"namespace outside pattern", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: kube-system}", true, Scope{}, "namespace"},
		{"denied namespace", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: agent-admin}", true, Scope{}, "namespace"},
		{"outside session scope", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: agent-2}", true, Scope{Namespaces: []string{"agent-1"}}, "namespace"},
		{"missing namespace", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c}", true, Scope{}, "namespace"},
		{"reserved label", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: agent-1, labels: {gitopsctl.io/app: other}}", true, Scope{}, "reserved-label"},
		{"too many replicas", strings.Replace(deployment(goodPodSpec), "replicas: 2", "replicas: 50", 1), true, Scope{}, "max-replicas"},
		{"privileged", deployment(strings.Replace(goodPodSpec, "{runAsNonRoot: true}", "{runAsNonRoot: true, privileged: true}", 1)), true, Scope{}, "privileged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testPolicy(t).Check([]Item{{Object: object(t, tc.manifest), Namespaced: tc.namespaced}}, tc.scope)
			if !strings.Contains(rules(got), tc.wantRule) {
				t.Fatalf("expected rule %q, got %q (%v)", tc.wantRule, rules(got), got)
			}
		})
	}
}

func TestCheck_PodSpecRules(t *testing.T) {
	cases := []struct {
		name     string
		podSpec  string
		wantRule string
	}{
		{"host network", "      hostNetwork: true\n" + goodPodSpec, "host-namespace"},
		{"host pid", "      hostPID: true\n" + goodPodSpec, "host-namespace"},
		{"host path", "      volumes:\n      - name: v\n        hostPath: {path: /}\n" + goodPodSpec, "host-path"},
		{"escalation", strings.Replace(goodPodSpec, "{runAsNonRoot: true}", "{runAsNonRoot: true, allowPrivilegeEscalation: true}", 1), "privilege-escalation"},
		{"capabilities", strings.Replace(goodPodSpec, "{runAsNonRoot: true}", "{runAsNonRoot: true, capabilities: {add: [NET_ADMIN]}}", 1), "capabilities"},
		{"root user", strings.Replace(goodPodSpec, "{runAsNonRoot: true}", "{runAsUser: 0}", 1), "run-as-non-root"},
		{"no security context", strings.Replace(goodPodSpec, "        securityContext: {runAsNonRoot: true}\n", "", 1), "run-as-non-root"},
		{"no limits", strings.Replace(goodPodSpec, "        resources: {limits: {cpu: 100m, memory: 64Mi}}\n", "", 1), "resource-limits"},
		{"missing memory limit", strings.Replace(goodPodSpec, ", memory: 64Mi", "", 1), "resource-limits"},
		{"registry", strings.Replace(goodPodSpec, "ghcr.io/acme/app:1", "evil.io/acme/app:1", 1), "image-registry"},
		{"registry prefix lookalike", strings.Replace(goodPodSpec, "ghcr.io/acme/app:1", "ghcr.io/acme-evil/app:1", 1), "image-registry"},
		{"host port", strings.Replace(goodPodSpec, "        resources", "        ports: [{containerPort: 80, hostPort: 80}]\n        resources", 1), "host-port"},
		{"init container", goodPodSpec + "      initContainers:\n      - name: init\n        image: evil.io/x\n", "image-registry"},
		{"ephemeral container", goodPodSpec + "      ephemeralContainers:\n      - name: dbg\n        image: ghcr.io/acme/x\n        securityContext: {privileged: true}\n", "privileged"},
		{"malformed container", goodPodSpec + "      initContainers: [oops]\n", "pod-spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testPolicy(t).Check([]Item{{Object: object(t, deployment(tc.podSpec)), Namespaced: true}}, Scope{})
			if !strings.Contains(rules(got), tc.wantRule) {
				t.Fatalf("expected rule %q, got %q", tc.wantRule, rules(got))
			}
		})
	}
}

func TestCheck_PodLevelNonRootCoversContainers(t *testing.T) {
	spec := "      securityContext: {runAsNonRoot: true}\n" + strings.Replace(goodPodSpec, "        securityContext: {runAsNonRoot: true}\n", "", 1)
	got := testPolicy(t).Check([]Item{{Object: object(t, deployment(spec)), Namespaced: true}}, Scope{})
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %v", got)
	}
}

func TestCheck_DockerHubShorthandMatchesDockerIO(t *testing.T) {
	spec := strings.Replace(goodPodSpec, "ghcr.io/acme/app:1", "nginx:1.27", 1)
	got := testPolicy(t).Check([]Item{{Object: object(t, deployment(spec)), Namespaced: true}}, Scope{})
	if len(got) != 0 {
		t.Fatalf("expected docker hub image to match docker.io, got %v", got)
	}
}

func TestCheck_CronJobAndPod(t *testing.T) {
	cron := `
apiVersion: batch/v1
kind: CronJob
metadata: {name: c, namespace: agent-1}
spec:
  jobTemplate:
    spec:
      template:
        spec:
          hostNetwork: true
          containers: []
`
	pod := "apiVersion: v1\nkind: Pod\nmetadata: {name: p, namespace: agent-1}\nspec:\n  hostPID: true\n  containers: []\n"
	p := testPolicy(t)
	if got := rules(p.Check([]Item{{Object: object(t, cron), Namespaced: true}}, Scope{})); !strings.Contains(got, "host-namespace") {
		t.Fatalf("cronjob: %q", got)
	}
	if got := rules(p.Check([]Item{{Object: object(t, pod), Namespaced: true}}, Scope{})); !strings.Contains(got, "host-namespace") {
		t.Fatalf("pod: %q", got)
	}
}

func TestCheck_MissingPodSpec(t *testing.T) {
	obj := object(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: d, namespace: agent-1}\nspec: {}")
	if got := rules(testPolicy(t).Check([]Item{{Object: obj, Namespaced: true}}, Scope{})); !strings.Contains(got, "pod-spec") {
		t.Fatalf("got %q", got)
	}
}

func TestCheck_MaxObjects(t *testing.T) {
	var items []Item
	for i := 0; i < 6; i++ {
		items = append(items, Item{Object: object(t, "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: agent-1}"), Namespaced: true})
	}
	if got := rules(testPolicy(t).Check(items, Scope{})); got != "max-objects" {
		t.Fatalf("got %q", got)
	}
}

func TestParse_FailsClosed(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"no kinds":            "allowed_namespaces: [a]",
		"no namespaces":       "allowed_kinds: [ConfigMap]",
		"unknown field":       validPolicy + "allow_everything: true\n",
		"negative limit":      validPolicy + "max_objects: -1\n",
		"forbidden kind":      "allowed_kinds: [rbac.authorization.k8s.io/ClusterRole]\nallowed_namespaces: [a]",
		"forbidden core kind": "allowed_kinds: [Namespace]\nallowed_namespaces: [a]",
		"bad pattern":         "allowed_kinds: [ConfigMap]\nallowed_namespaces: ['[']",
		"not yaml":            "::: not yaml",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoad(t *testing.T) {
	if _, err := Load("/nonexistent/policy.yaml"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	file := t.TempDir() + "/policy.yaml"
	writeFile(t, file, validPolicy)
	if _, err := Load(file); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestViolationError(t *testing.T) {
	if got := (Violation{Rule: "r", Object: "ConfigMap a/b", Message: "m"}).Error(); got != "policy r: ConfigMap a/b: m" {
		t.Fatalf("got %q", got)
	}
	if got := (Violation{Rule: "r", Message: "m"}).Error(); got != "policy r: m" {
		t.Fatalf("got %q", got)
	}
}

func TestNamespaceAllowed(t *testing.T) {
	p := testPolicy(t)
	for ns, want := range map[string]bool{"agent-1": true, "agent-admin": false, "kube-system": false, "": false} {
		if got := p.NamespaceAllowed(ns); got != want {
			t.Errorf("NamespaceAllowed(%q) = %v, want %v", ns, got, want)
		}
	}
}
