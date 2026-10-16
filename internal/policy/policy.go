package policy

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const reservedLabelPrefix = "gitopsctl.io/"

var alwaysDeniedKinds = map[string]struct{}{
	"Namespace":                      {},
	"CustomResourceDefinition":       {},
	"ClusterRole":                    {},
	"ClusterRoleBinding":             {},
	"Role":                           {},
	"RoleBinding":                    {},
	"ValidatingWebhookConfiguration": {},
	"MutatingWebhookConfiguration":   {},
	"PersistentVolume":               {},
}

type Policy struct {
	AllowedKinds      []string `json:"allowed_kinds"`
	AllowedNamespaces []string `json:"allowed_namespaces"`
	DeniedNamespaces  []string `json:"denied_namespaces"`
	AllowedRegistries []string `json:"allowed_registries"`
	MaxObjects        int      `json:"max_objects"`
	MaxReplicas       int64    `json:"max_replicas"`
}

type Scope struct {
	Namespaces []string
}

type Item struct {
	Object     *unstructured.Unstructured
	Namespaced bool
}

type Violation struct {
	Rule    string
	Object  string
	Message string
}

func (v Violation) Error() string {
	if v.Object == "" {
		return fmt.Sprintf("policy %s: %s", v.Rule, v.Message)
	}
	return fmt.Sprintf("policy %s: %s: %s", v.Rule, v.Object, v.Message)
}

func Load(file string) (*Policy, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}
	return Parse(data)
}

func Parse(data []byte) (*Policy, error) {
	p := &Policy{}
	if err := yaml.UnmarshalStrict(data, p); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Policy) Validate() error {
	if len(p.AllowedKinds) == 0 {
		return fmt.Errorf("invalid policy: allowed_kinds must not be empty")
	}
	if len(p.AllowedNamespaces) == 0 {
		return fmt.Errorf("invalid policy: allowed_namespaces must not be empty")
	}
	if p.MaxObjects < 0 || p.MaxReplicas < 0 {
		return fmt.Errorf("invalid policy: limits must not be negative")
	}
	for _, kind := range p.AllowedKinds {
		if _, denied := alwaysDeniedKinds[kind[strings.LastIndex(kind, "/")+1:]]; denied {
			return fmt.Errorf("invalid policy: kind %q can never be allowed", kind)
		}
	}
	for _, pattern := range append(append([]string{}, p.AllowedNamespaces...), p.DeniedNamespaces...) {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid policy: namespace pattern %q: %w", pattern, err)
		}
	}
	return nil
}

func (p *Policy) Check(items []Item, scope Scope) []Violation {
	var violations []Violation
	if p.MaxObjects > 0 && len(items) > p.MaxObjects {
		violations = append(violations, Violation{Rule: "max-objects", Message: fmt.Sprintf("%d objects exceed the limit of %d", len(items), p.MaxObjects)})
	}
	for _, item := range items {
		violations = append(violations, p.checkItem(item, scope)...)
	}
	return violations
}

func (p *Policy) checkItem(item Item, scope Scope) []Violation {
	obj := item.Object
	id := identify(obj)
	var violations []Violation
	add := func(rule, format string, args ...any) {
		violations = append(violations, Violation{Rule: rule, Object: id, Message: fmt.Sprintf(format, args...)})
	}

	kind := obj.GetKind()
	if _, denied := alwaysDeniedKinds[kind]; denied {
		add("kind", "kind %s is never allowed", kind)
	} else if !p.kindAllowed(obj) {
		add("kind", "kind %s is not in allowed_kinds", qualifiedKind(obj))
	}

	if !item.Namespaced {
		add("cluster-scoped", "cluster-scoped objects are not allowed")
	} else if reason := p.namespaceViolation(obj.GetNamespace(), scope); reason != "" {
		add("namespace", "%s", reason)
	}

	for key := range obj.GetLabels() {
		if strings.HasPrefix(key, reservedLabelPrefix) {
			add("reserved-label", "label %q is reserved", key)
		}
	}

	for _, v := range p.checkWorkload(obj) {
		violations = append(violations, Violation{Rule: v.Rule, Object: id, Message: v.Message})
	}
	return violations
}

func (p *Policy) kindAllowed(obj *unstructured.Unstructured) bool {
	qualified := qualifiedKind(obj)
	for _, allowed := range p.AllowedKinds {
		if allowed == qualified {
			return true
		}
	}
	return false
}

func qualifiedKind(obj *unstructured.Unstructured) string {
	gvk := obj.GroupVersionKind()
	if gvk.Group == "" {
		return gvk.Kind
	}
	return gvk.Group + "/" + gvk.Kind
}

func (p *Policy) namespaceViolation(ns string, scope Scope) string {
	if ns == "" {
		return "namespace is required"
	}
	if matchesAny(p.DeniedNamespaces, ns) {
		return fmt.Sprintf("namespace %q is denied", ns)
	}
	if !matchesAny(p.AllowedNamespaces, ns) {
		return fmt.Sprintf("namespace %q is not in allowed_namespaces", ns)
	}
	if len(scope.Namespaces) > 0 && !slices.Contains(scope.Namespaces, ns) {
		return fmt.Sprintf("namespace %q is outside the session scope", ns)
	}
	return ""
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if ok, err := path.Match(pattern, value); err == nil && ok {
			return true
		}
	}
	return false
}

func identify(obj *unstructured.Unstructured) string {
	if ns := obj.GetNamespace(); ns != "" {
		return fmt.Sprintf("%s %s/%s", obj.GetKind(), ns, obj.GetName())
	}
	return fmt.Sprintf("%s %s", obj.GetKind(), obj.GetName())
}

func (p *Policy) NamespaceAllowed(ns string) bool {
	return p.namespaceViolation(ns, Scope{}) == ""
}
