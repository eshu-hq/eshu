// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
)

const (
	irsaRoleAnnotationKey          = "eks.amazonaws.com/role-arn"
	gcpServiceAccountAnnotationKey = "iam.gke.io/gcp-service-account"
)

// ListServiceAccounts lists ServiceAccounts across all namespaces.
func (a *Adapter) ListServiceAccounts(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.ServiceAccountObject], error) {
	var items []kuberneteslive.ServiceAccountObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.CoreV1().ServiceAccounts(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			account := &list.Items[i]
			items = append(items, serviceAccountObject(account))
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.ServiceAccountObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.ServiceAccountObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListRoles lists namespace-scoped RBAC Roles across all namespaces.
func (a *Adapter) ListRoles(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.RBACRoleObject], error) {
	var items []kuberneteslive.RBACRoleObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.RbacV1().Roles(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			role := &list.Items[i]
			items = append(items, kuberneteslive.RBACRoleObject{
				Meta:  objectMeta("rbac.authorization.k8s.io", "v1", "roles", role.ObjectMeta),
				Kind:  kuberneteslive.RBACRoleKindRole,
				Rules: policyRules(role.Rules),
			})
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.RBACRoleObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.RBACRoleObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListClusterRoles lists cluster-scoped RBAC ClusterRoles.
func (a *Adapter) ListClusterRoles(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.RBACRoleObject], error) {
	var items []kuberneteslive.RBACRoleObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.RbacV1().ClusterRoles().List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			role := &list.Items[i]
			items = append(items, kuberneteslive.RBACRoleObject{
				Meta:  objectMeta("rbac.authorization.k8s.io", "v1", "clusterroles", role.ObjectMeta),
				Kind:  kuberneteslive.RBACRoleKindClusterRole,
				Rules: policyRules(role.Rules),
			})
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.RBACRoleObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.RBACRoleObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListRoleBindings lists namespace-scoped RBAC RoleBindings across all
// namespaces.
func (a *Adapter) ListRoleBindings(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.RBACBindingObject], error) {
	var items []kuberneteslive.RBACBindingObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.RbacV1().RoleBindings(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			binding := &list.Items[i]
			items = append(items, rbacBindingObject(
				objectMeta("rbac.authorization.k8s.io", "v1", "rolebindings", binding.ObjectMeta),
				kuberneteslive.BindingKindRoleBinding,
				binding.RoleRef,
				binding.Subjects,
			))
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.RBACBindingObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.RBACBindingObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListClusterRoleBindings lists cluster-scoped RBAC ClusterRoleBindings.
func (a *Adapter) ListClusterRoleBindings(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.RBACBindingObject], error) {
	var items []kuberneteslive.RBACBindingObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.RbacV1().ClusterRoleBindings().List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			binding := &list.Items[i]
			items = append(items, rbacBindingObject(
				objectMeta("rbac.authorization.k8s.io", "v1", "clusterrolebindings", binding.ObjectMeta),
				kuberneteslive.BindingKindClusterRoleBinding,
				binding.RoleRef,
				binding.Subjects,
			))
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.RBACBindingObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.RBACBindingObject]{Items: items, Partial: partial, Reason: reason}, nil
}

func serviceAccountObject(account *corev1.ServiceAccount) kuberneteslive.ServiceAccountObject {
	annotationKeys := make([]string, 0, len(account.Annotations))
	for key := range account.Annotations {
		annotationKeys = append(annotationKeys, key)
	}
	sort.Strings(annotationKeys)
	return kuberneteslive.ServiceAccountObject{
		Meta:                        objectMeta("", "v1", "serviceaccounts", account.ObjectMeta),
		AnnotationKeys:              annotationKeys,
		IRSAAnnotation:              account.Annotations[irsaRoleAnnotationKey],
		GCPServiceAccountAnnotation: account.Annotations[gcpServiceAccountAnnotationKey],
		AutomountToken:              account.AutomountServiceAccountToken,
		SecretRefCount:              len(account.Secrets),
		ImagePullSecretRefCount:     len(account.ImagePullSecrets),
	}
}

func policyRules(rules []rbacv1.PolicyRule) []kuberneteslive.RBACRuleSummary {
	if len(rules) == 0 {
		return nil
	}
	output := make([]kuberneteslive.RBACRuleSummary, 0, len(rules))
	for _, rule := range rules {
		output = append(output, kuberneteslive.RBACRuleSummary{
			Verbs:                  copyStrings(rule.Verbs),
			APIGroups:              copyStrings(rule.APIGroups),
			Resources:              copyStrings(rule.Resources),
			ResourceNameCount:      len(rule.ResourceNames),
			ResourceNamesPresent:   len(rule.ResourceNames) > 0,
			NonResourceURLCount:    len(rule.NonResourceURLs),
			NonResourceURLsPresent: len(rule.NonResourceURLs) > 0,
		})
	}
	return output
}

func rbacBindingObject(
	meta kuberneteslive.ObjectMeta,
	kind string,
	roleRef rbacv1.RoleRef,
	subjects []rbacv1.Subject,
) kuberneteslive.RBACBindingObject {
	return kuberneteslive.RBACBindingObject{
		Meta:            meta,
		Kind:            kind,
		RoleRefKind:     roleRef.Kind,
		RoleRefAPIGroup: roleRef.APIGroup,
		RoleRefName:     roleRef.Name,
		Subjects:        rbacSubjects(subjects),
	}
}

func rbacSubjects(subjects []rbacv1.Subject) []kuberneteslive.RBACSubject {
	if len(subjects) == 0 {
		return nil
	}
	output := make([]kuberneteslive.RBACSubject, 0, len(subjects))
	for _, subject := range subjects {
		output = append(output, kuberneteslive.RBACSubject{
			Kind:      subject.Kind,
			APIGroup:  subject.APIGroup,
			Namespace: subject.Namespace,
			Name:      subject.Name,
		})
	}
	return output
}

func projectedServiceAccountToken(volumes []corev1.Volume) bool {
	for _, volume := range volumes {
		if volume.Projected == nil {
			continue
		}
		for _, source := range volume.Projected.Sources {
			if source.ServiceAccountToken != nil {
				return true
			}
		}
	}
	return false
}

func copyStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := append([]string(nil), input...)
	sort.Strings(output)
	return output
}
