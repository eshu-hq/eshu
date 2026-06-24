// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"
)

type normalizedRBACRole struct {
	kind      string
	namespace string
	name      string
	scope     string
}

type normalizedRBACBinding struct {
	kind             string
	namespace        string
	name             string
	scope            string
	roleRefKind      string
	roleRefNamespace string
	roleRefName      string
}

func normalizeRBACRoleIdentity(kind, namespace, name string) (normalizedRBACRole, error) {
	role := normalizedRBACRole{
		kind:      strings.TrimSpace(kind),
		namespace: strings.TrimSpace(namespace),
		name:      strings.TrimSpace(name),
	}
	if role.name == "" {
		return normalizedRBACRole{}, fmt.Errorf("kubernetes rbac role observation requires name")
	}
	switch role.kind {
	case RBACRoleKindRole:
		if role.namespace == "" {
			return normalizedRBACRole{}, fmt.Errorf("kubernetes role observation requires namespace")
		}
		role.scope = BindingScopeNamespace
	case RBACRoleKindClusterRole:
		role.namespace = ""
		role.scope = BindingScopeCluster
	default:
		return normalizedRBACRole{}, fmt.Errorf("kubernetes rbac role observation has unsupported role_kind %q", role.kind)
	}
	return role, nil
}

func normalizeRBACBindingIdentity(
	kind string,
	namespace string,
	name string,
	roleRefKind string,
	roleRefName string,
) (normalizedRBACBinding, error) {
	binding := normalizedRBACBinding{
		kind:        strings.TrimSpace(kind),
		namespace:   strings.TrimSpace(namespace),
		name:        strings.TrimSpace(name),
		roleRefKind: strings.TrimSpace(roleRefKind),
		roleRefName: strings.TrimSpace(roleRefName),
	}
	if binding.name == "" {
		return normalizedRBACBinding{}, fmt.Errorf("kubernetes rbac binding observation requires name")
	}
	switch binding.kind {
	case BindingKindRoleBinding:
		if binding.namespace == "" {
			return normalizedRBACBinding{}, fmt.Errorf("kubernetes rolebinding observation requires namespace")
		}
		binding.scope = BindingScopeNamespace
	case BindingKindClusterRoleBinding:
		binding.namespace = ""
		binding.scope = BindingScopeCluster
	default:
		return normalizedRBACBinding{}, fmt.Errorf("kubernetes rbac binding observation has unsupported binding_kind %q", binding.kind)
	}
	if binding.roleRefName == "" {
		return normalizedRBACBinding{}, fmt.Errorf("kubernetes rbac binding observation requires role_ref_name")
	}
	switch binding.roleRefKind {
	case RBACRoleKindRole:
		if binding.kind != BindingKindRoleBinding {
			return normalizedRBACBinding{}, fmt.Errorf("kubernetes clusterrolebinding observation cannot reference a Role")
		}
		binding.roleRefNamespace = binding.namespace
	case RBACRoleKindClusterRole:
		binding.roleRefNamespace = ""
	default:
		return normalizedRBACBinding{}, fmt.Errorf("kubernetes rbac binding observation has unsupported role_ref_kind %q", binding.roleRefKind)
	}
	return binding, nil
}
