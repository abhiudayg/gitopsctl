package k8s

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type PlanAction string

const (
	PlanCreate    PlanAction = "create"
	PlanUpdate    PlanAction = "update"
	PlanUnchanged PlanAction = "unchanged"
	PlanPrune     PlanAction = "prune"
)

type PlanItem struct {
	Resource ResourceMetadata
	Action   PlanAction
}

// Plan reports what ApplyManifests would do without modifying the cluster.
// Resources in previous that are absent from the rendered manifests are reported as prune candidates.
func (cs *ClientSet) Plan(ctx context.Context, manifestsDir, appName, clusterName string, previous []ResourceMetadata) ([]PlanItem, []error) {
	docs, errs := cs.renderManifests(manifestsDir)
	if violations := cs.checkPolicy(docs); len(violations) > 0 {
		return nil, append(errs, violations...)
	}

	var items []PlanItem
	var rendered []ResourceMetadata
	for _, doc := range docs {
		resources, docErrs := cs.processYAMLData(ctx, doc.data, doc.source, appName, clusterName, &items)
		rendered = append(rendered, resources...)
		errs = append(errs, docErrs...)
	}
	for _, stale := range staleResources(previous, rendered) {
		items = append(items, PlanItem{Resource: stale, Action: PlanPrune})
	}
	return items, errs
}

func planAction(desired, live *unstructured.Unstructured, getErr error) PlanAction {
	if getErr != nil {
		return PlanCreate
	}
	if isSubset(desiredWithoutStatus(desired), live.Object) {
		return PlanUnchanged
	}
	return PlanUpdate
}

func desiredWithoutStatus(obj *unstructured.Unstructured) map[string]any {
	content := make(map[string]any, len(obj.Object))
	for k, v := range obj.Object {
		if k != "status" {
			content[k] = v
		}
	}
	return content
}

// isSubset reports whether every field set in desired is present with the same value in live,
// so fields defaulted by the API server do not register as drift.
func isSubset(desired, live any) bool {
	switch d := desired.(type) {
	case map[string]any:
		l, ok := live.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range d {
			if k == "creationTimestamp" && v == nil {
				continue
			}
			lv, present := l[k]
			if !present || !isSubset(v, lv) {
				return false
			}
		}
		return true
	case []any:
		l, ok := live.([]any)
		if !ok || len(l) != len(d) {
			return false
		}
		for i := range d {
			if !isSubset(d[i], l[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(desired, live) || fmt.Sprint(desired) == fmt.Sprint(live)
	}
}
