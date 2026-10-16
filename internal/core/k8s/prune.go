package k8s

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const DefaultMaxPrune = 10

var protectedKinds = map[string]bool{
	"Namespace":                true,
	"CustomResourceDefinition": true,
	"PersistentVolume":         true,
	"PersistentVolumeClaim":    true,
}

func staleResources(previous, current []ResourceMetadata) []ResourceMetadata {
	keep := make(map[ResourceMetadata]bool, len(current))
	for _, r := range current {
		keep[r] = true
	}
	var stale []ResourceMetadata
	for _, r := range previous {
		if !keep[r] {
			stale = append(stale, r)
		}
	}
	return stale
}

// Prune deletes resources that were applied for appName previously but are absent from current.
// Only objects labelled as owned by appName are deleted; protected kinds are never deleted.
// It returns the stale resources that must stay tracked because they could not be settled.
func (cs *ClientSet) Prune(ctx context.Context, appName string, previous, current []ResourceMetadata, maxDeletions int) ([]ResourceMetadata, error) {
	stale := staleResources(previous, current)
	if len(stale) == 0 {
		return nil, nil
	}
	if len(stale) > maxDeletions {
		return stale, fmt.Errorf("refusing to prune %d resources (limit %d)", len(stale), maxDeletions)
	}

	var retained []ResourceMetadata
	var errs []error
	for _, r := range stale {
		if err := cs.pruneOne(ctx, appName, r); err != nil {
			retained = append(retained, r)
			errs = append(errs, fmt.Errorf("prune %s %s/%s: %w", r.Kind, r.Namespace, r.Name, err))
		}
	}
	return retained, errors.Join(errs...)
}

func (cs *ClientSet) pruneOne(ctx context.Context, appName string, r ResourceMetadata) error {
	if protectedKinds[r.Kind] {
		cs.logger.Warn("Not pruning protected resource kind", zap.String("kind", r.Kind), zap.String("name", r.Name))
		return nil
	}

	gvk := schema.GroupVersionKind{Group: r.Group, Version: r.Version, Kind: r.Kind}
	mapping, err := cs.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if !cs.isNamespaceAllowed(r.Namespace) {
			return fmt.Errorf("namespace %q is not in the allowed list for this cluster", r.Namespace)
		}
		dr = cs.dynamicClient.Resource(mapping.Resource).Namespace(r.Namespace)
	} else {
		dr = cs.dynamicClient.Resource(mapping.Resource)
	}

	obj, err := dr.Get(ctx, r.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	labels := obj.GetLabels()
	if labels[LabelManagedBy] != ManagerName || labels[LabelApp] != appName {
		cs.logger.Warn("Not pruning resource not owned by this app", zap.String("kind", r.Kind), zap.String("name", r.Name), zap.String("app", appName))
		return nil
	}

	if err := dr.Delete(ctx, r.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	cs.logger.Info("Pruned resource", zap.String("kind", r.Kind), zap.String("name", r.Name), zap.String("namespace", r.Namespace))
	return nil
}
