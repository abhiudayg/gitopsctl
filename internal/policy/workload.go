package policy

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type finding struct {
	Rule    string
	Message string
}

var podSpecPaths = map[string][]string{
	"Pod":         {"spec"},
	"Deployment":  {"spec", "template", "spec"},
	"StatefulSet": {"spec", "template", "spec"},
	"DaemonSet":   {"spec", "template", "spec"},
	"ReplicaSet":  {"spec", "template", "spec"},
	"Job":         {"spec", "template", "spec"},
	"CronJob":     {"spec", "jobTemplate", "spec", "template", "spec"},
}

var hostFlags = []string{"hostNetwork", "hostPID", "hostIPC"}

var containerLists = []string{"containers", "initContainers", "ephemeralContainers"}

func (p *Policy) checkWorkload(obj *unstructured.Unstructured) []finding {
	var findings []finding
	add := func(rule, format string, args ...any) {
		findings = append(findings, finding{Rule: rule, Message: fmt.Sprintf(format, args...)})
	}

	if p.MaxReplicas > 0 {
		if replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas"); err == nil && found && replicas > p.MaxReplicas {
			add("max-replicas", "%d replicas exceed the limit of %d", replicas, p.MaxReplicas)
		}
	}

	fields, isWorkload := podSpecPaths[obj.GetKind()]
	if !isWorkload {
		return findings
	}
	spec, found, err := unstructured.NestedMap(obj.Object, fields...)
	if err != nil || !found {
		add("pod-spec", "pod spec is missing or malformed")
		return findings
	}

	for _, flag := range hostFlags {
		if enabled, _, _ := unstructured.NestedBool(spec, flag); enabled {
			add("host-namespace", "%s is not allowed", flag)
		}
	}
	if volumes, _, _ := unstructured.NestedSlice(spec, "volumes"); volumes != nil {
		for _, v := range volumes {
			if volume, ok := v.(map[string]any); ok {
				if _, hostPath := volume["hostPath"]; hostPath {
					add("host-path", "hostPath volume %q is not allowed", volume["name"])
				}
			}
		}
	}

	podNonRoot := nonRoot(spec)
	for _, list := range containerLists {
		containers, _, _ := unstructured.NestedSlice(spec, list)
		for _, c := range containers {
			container, ok := c.(map[string]any)
			if !ok {
				add("pod-spec", "%s entry is malformed", list)
				continue
			}
			findings = append(findings, p.checkContainer(container, podNonRoot)...)
		}
	}
	return findings
}

func (p *Policy) checkContainer(container map[string]any, podNonRoot bool) []finding {
	name, _ := container["name"].(string)
	var findings []finding
	add := func(rule, format string, args ...any) {
		findings = append(findings, finding{Rule: rule, Message: fmt.Sprintf("container %q: "+format, append([]any{name}, args...)...)})
	}

	if image, _ := container["image"].(string); !p.registryAllowed(image) {
		add("image-registry", "image %q is not from an allowed registry", image)
	}

	sc, _, _ := unstructured.NestedMap(container, "securityContext")
	if privileged, _, _ := unstructured.NestedBool(sc, "privileged"); privileged {
		add("privileged", "privileged containers are not allowed")
	}
	if escalation, _, _ := unstructured.NestedBool(sc, "allowPrivilegeEscalation"); escalation {
		add("privilege-escalation", "allowPrivilegeEscalation must not be true")
	}
	if added, _, _ := unstructured.NestedSlice(sc, "capabilities", "add"); len(added) > 0 {
		add("capabilities", "adding capabilities is not allowed")
	}
	if !podNonRoot && !nonRoot(container) {
		add("run-as-non-root", "must set runAsNonRoot: true or a non-zero runAsUser")
	}

	for _, resource := range []string{"cpu", "memory"} {
		if limit, _, _ := unstructured.NestedString(container, "resources", "limits", resource); limit == "" {
			add("resource-limits", "resources.limits.%s is required", resource)
		}
	}

	ports, _, _ := unstructured.NestedSlice(container, "ports")
	for _, port := range ports {
		if m, ok := port.(map[string]any); ok {
			if _, hostPort := m["hostPort"]; hostPort {
				add("host-port", "hostPort is not allowed")
			}
		}
	}
	return findings
}

func nonRoot(holder map[string]any) bool {
	sc, _, _ := unstructured.NestedMap(holder, "securityContext")
	if runAsNonRoot, found, _ := unstructured.NestedBool(sc, "runAsNonRoot"); found {
		return runAsNonRoot
	}
	user, found, _ := unstructured.NestedInt64(sc, "runAsUser")
	return found && user > 0
}

func (p *Policy) registryAllowed(image string) bool {
	if len(p.AllowedRegistries) == 0 {
		return true
	}
	normalized := normalizeImage(image)
	for _, registry := range p.AllowedRegistries {
		if strings.HasPrefix(normalized, strings.TrimSuffix(normalizeImage(registry+"/x"), "x")) {
			return true
		}
	}
	return false
}

func normalizeImage(image string) string {
	first, _, hasSlash := strings.Cut(image, "/")
	if hasSlash && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return image
	}
	return "docker.io/" + image
}
