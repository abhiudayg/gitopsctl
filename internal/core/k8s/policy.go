package k8s

import (
	"fmt"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/policy"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const fallbackNamespace = "default"

// WithPolicy returns a copy of the client set that rejects any manifest set violating p.
func (cs *ClientSet) WithPolicy(p *policy.Policy, scope policy.Scope) *ClientSet {
	enforced := *cs
	enforced.policy = p
	enforced.policyScope = scope
	return &enforced
}

func (cs *ClientSet) defaultNamespace() string {
	if cs.policy != nil && len(cs.policyScope.Namespaces) == 1 {
		return cs.policyScope.Namespaces[0]
	}
	return fallbackNamespace
}

// checkPolicy evaluates every rendered object before anything is written, so a violation blocks the whole apply.
func (cs *ClientSet) checkPolicy(docs []manifestDoc) []error {
	if cs.policy == nil {
		return nil
	}
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	var items []policy.Item
	var errs []error
	for _, doc := range docs {
		for i, objStr := range strings.Split(string(doc.data), "\n---") {
			trimmed := strings.TrimSpace(objStr)
			if trimmed == "" {
				continue
			}
			obj := &unstructured.Unstructured{}
			_, gvk, err := decoder.Decode([]byte(trimmed), nil, obj)
			if err != nil {
				errs = append(errs, policy.Violation{Rule: "decode", Message: fmt.Sprintf("%s (doc %d): %v", doc.source, i, err)})
				continue
			}
			mapping, err := cs.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if err != nil {
				errs = append(errs, policy.Violation{Rule: "kind", Object: fmt.Sprintf("%s %s", gvk.Kind, obj.GetName()), Message: fmt.Sprintf("unknown kind: %v", err)})
				continue
			}
			namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
			if namespaced && obj.GetNamespace() == "" {
				obj.SetNamespace(cs.defaultNamespace())
			}
			items = append(items, policy.Item{Object: obj, Namespaced: namespaced})
		}
	}
	for _, v := range cs.policy.Check(items, cs.policyScope) {
		errs = append(errs, v)
	}
	return errs
}
