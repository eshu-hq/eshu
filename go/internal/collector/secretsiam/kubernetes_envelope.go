// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewKubernetesServiceAccountEnvelope builds a k8s_service_account source fact.
// It fingerprints namespace and ServiceAccount names by default while
// preserving deterministic join keys for reducers.
func NewKubernetesServiceAccountEnvelope(observation KubernetesServiceAccountObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	joinKey, err := serviceAccountJoinKey(observation.Context, observation.Namespace, observation.Name)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.KubernetesServiceAccountFactKind, map[string]any{
		"cluster_id":               observation.Context.ClusterID,
		"service_account_join_key": joinKey,
		"uid":                      fingerprintKubernetesValue("uid", observation.UID),
	})
	payload := kubernetesPayload(observation.Context)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.Name)
	payload["service_account_join_key"] = joinKey
	payload["uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.UID)
	payload["annotation_keys"] = normalizeKeyList(observation.AnnotationKeys)
	payload["annotation_count"] = len(normalizeKeyList(observation.AnnotationKeys))
	payload["automount_token"] = normalizeBoolState(observation.AutomountToken)
	payload["secret_ref_count"] = observation.SecretRefCount
	payload["image_pull_secret_ref_count"] = observation.ImagePullSecretRefCount
	payload["resource_version_fingerprint"] = fingerprintKubernetesValue("resource_version", observation.ResourceVersion)
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesServiceAccountFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewKubernetesServiceAccountTokenPostureEnvelope builds a token posture source
// fact without carrying token, Secret, or projected volume values.
func NewKubernetesServiceAccountTokenPostureEnvelope(
	observation KubernetesServiceAccountTokenPostureObservation,
) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	joinKey, err := serviceAccountJoinKey(observation.Context, observation.Namespace, observation.ServiceAccountName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.KubernetesServiceAccountTokenPostureFactKind, map[string]any{
		"cluster_id":               observation.Context.ClusterID,
		"service_account_join_key": joinKey,
		"uid":                      fingerprintKubernetesValue("uid", observation.ServiceAccountUID),
	})
	payload := kubernetesPayload(observation.Context)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.ServiceAccountName)
	payload["service_account_join_key"] = joinKey
	payload["service_account_uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.ServiceAccountUID)
	payload["automount_token"] = normalizeBoolState(observation.AutomountToken)
	payload["secret_ref_count"] = observation.SecretRefCount
	payload["image_pull_secret_ref_count"] = observation.ImagePullSecretRefCount
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesServiceAccountTokenPostureFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewKubernetesRBACRoleEnvelope builds a k8s_rbac_role source fact for a Role
// or ClusterRole with bounded rule summaries.
func NewKubernetesRBACRoleEnvelope(observation KubernetesRBACRoleObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	role, err := normalizeRBACRoleIdentity(observation.RoleKind, observation.Namespace, observation.Name)
	if err != nil {
		return facts.Envelope{}, err
	}
	roleKey := rbacRoleJoinKey(observation.Context, role.kind, role.namespace, role.name)
	stableKey := facts.StableID(facts.KubernetesRBACRoleFactKind, map[string]any{
		"cluster_id": observation.Context.ClusterID,
		"role_key":   roleKey,
		"uid":        fingerprintKubernetesValue("uid", observation.UID),
	})
	payload := kubernetesPayload(observation.Context)
	payload["role_kind"] = role.kind
	payload["role_scope"] = role.scope
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", role.namespace)
	payload["role_name_fingerprint"] = fingerprintKubernetesValue("rbac_role", role.name)
	payload["role_join_key"] = roleKey
	payload["uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.UID)
	payload["resource_version_fingerprint"] = fingerprintKubernetesValue("resource_version", observation.ResourceVersion)
	payload["rules"] = rbacRulePayloads(observation.Rules)
	payload["rule_count"] = len(observation.Rules)
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesRBACRoleFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewKubernetesRBACBindingEnvelope builds a k8s_rbac_binding source fact. It
// distinguishes namespaced RoleBindings from cluster-wide ClusterRoleBindings.
func NewKubernetesRBACBindingEnvelope(observation KubernetesRBACBindingObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	binding, err := normalizeRBACBindingIdentity(
		observation.BindingKind,
		observation.Namespace,
		observation.Name,
		observation.RoleRefKind,
		observation.RoleRefName,
	)
	if err != nil {
		return facts.Envelope{}, err
	}
	roleRefKey := rbacRoleJoinKey(
		observation.Context,
		binding.roleRefKind,
		binding.roleRefNamespace,
		binding.roleRefName,
	)
	stableKey := facts.StableID(facts.KubernetesRBACBindingFactKind, map[string]any{
		"binding_kind": binding.kind,
		"cluster_id":   observation.Context.ClusterID,
		"name":         fingerprintKubernetesValue("rbac_binding", binding.name),
		"namespace":    fingerprintKubernetesValue("namespace", binding.namespace),
		"uid":          fingerprintKubernetesValue("uid", observation.UID),
	})
	payload := kubernetesPayload(observation.Context)
	payload["binding_kind"] = binding.kind
	payload["binding_scope"] = binding.scope
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", binding.namespace)
	payload["binding_name_fingerprint"] = fingerprintKubernetesValue("rbac_binding", binding.name)
	payload["uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.UID)
	payload["resource_version_fingerprint"] = fingerprintKubernetesValue("resource_version", observation.ResourceVersion)
	payload["role_ref_kind"] = binding.roleRefKind
	payload["role_ref_api_group"] = strings.TrimSpace(observation.RoleRefAPIGroup)
	payload["role_ref_name_fingerprint"] = fingerprintKubernetesValue("rbac_role_ref", binding.roleRefName)
	payload["role_ref_join_key"] = roleRefKey
	payload["subject_count"] = maxInt(observation.SubjectCount, len(observation.Subjects))
	payload["subjects"] = rbacSubjectPayloads(observation.Context, observation.Subjects)
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesRBACBindingFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewKubernetesWorkloadIdentityUseEnvelope builds a
// k8s_workload_identity_use source fact for one workload's ServiceAccount
// reference.
func NewKubernetesWorkloadIdentityUseEnvelope(
	observation KubernetesWorkloadIdentityUseObservation,
) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	workloadObjectID := strings.TrimSpace(observation.WorkloadObjectID)
	if workloadObjectID == "" {
		return facts.Envelope{}, fmt.Errorf("kubernetes workload identity observation requires workload_object_id")
	}
	joinKey, err := serviceAccountJoinKey(observation.Context, observation.Namespace, observation.ServiceAccountName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.KubernetesWorkloadIdentityUseFactKind, map[string]any{
		"cluster_id":               observation.Context.ClusterID,
		"service_account_join_key": joinKey,
		"workload_object_id":       workloadObjectID,
	})
	payload := kubernetesPayload(observation.Context)
	payload["workload_object_id"] = workloadObjectID
	payload["workload_kind"] = strings.TrimSpace(observation.WorkloadKind)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.ServiceAccountName)
	payload["service_account_join_key"] = joinKey
	payload["service_account_uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.ServiceAccountUID)
	payload["projected_service_account_token"] = observation.ProjectedServiceAccountToken
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesWorkloadIdentityUseFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewEKSIRSAAnnotationEnvelope builds an eks_irsa_annotation source fact.
func NewEKSIRSAAnnotationEnvelope(observation EKSIRSAAnnotationObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	roleARN := strings.TrimSpace(observation.RoleARN)
	if roleARN == "" {
		return facts.Envelope{}, fmt.Errorf("eks irsa annotation observation requires role_arn")
	}
	joinKey, err := serviceAccountJoinKey(observation.Context, observation.Namespace, observation.ServiceAccountName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.EKSIRSAAnnotationFactKind, map[string]any{
		"cluster_id":               observation.Context.ClusterID,
		"role_arn":                 roleARN,
		"service_account_join_key": joinKey,
	})
	payload := kubernetesPayload(observation.Context)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.ServiceAccountName)
	payload["service_account_join_key"] = joinKey
	payload["service_account_uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.ServiceAccountUID)
	payload["role_arn"] = roleARN
	payload["annotation_present"] = observation.AnnotationPresent
	payload["web_identity_subject_fingerprint"] = WebIdentitySubjectFingerprint(
		kubernetesWebIdentitySubject(observation.Namespace, observation.ServiceAccountName),
	)
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.EKSIRSAAnnotationFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewEKSPodIdentityAssociationEnvelope builds an
// eks_pod_identity_association source fact when association evidence exists.
func NewEKSPodIdentityAssociationEnvelope(observation EKSPodIdentityAssociationObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	associationID := strings.TrimSpace(observation.AssociationID)
	roleARN := strings.TrimSpace(observation.RoleARN)
	if associationID == "" || roleARN == "" {
		return facts.Envelope{}, fmt.Errorf("eks pod identity association observation requires association_id and role_arn")
	}
	joinKey, err := serviceAccountJoinKey(observation.Context, observation.Namespace, observation.ServiceAccountName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.EKSPodIdentityAssociationFactKind, map[string]any{
		"association_id":           associationID,
		"cluster_id":               observation.Context.ClusterID,
		"role_arn":                 roleARN,
		"service_account_join_key": joinKey,
	})
	payload := kubernetesPayload(observation.Context)
	payload["association_id"] = associationID
	payload["cluster_name_fingerprint"] = fingerprintKubernetesValue("eks_cluster_name", observation.ClusterName)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.ServiceAccountName)
	payload["service_account_join_key"] = joinKey
	payload["role_arn"] = roleARN
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.EKSPodIdentityAssociationFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewKubernetesCoverageWarningEnvelope builds secrets_iam_coverage_warning
// evidence for partial or hidden Kubernetes source reads.
func NewKubernetesCoverageWarningEnvelope(observation KubernetesCoverageWarningObservation) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	sourceState := strings.TrimSpace(observation.SourceState)
	if warningKind == "" || sourceState == "" {
		return facts.Envelope{}, fmt.Errorf("kubernetes coverage warning requires warning_kind and source_state")
	}
	resourceScope := strings.TrimSpace(observation.ResourceScope)
	stableKey := facts.StableID(facts.SecretsIAMCoverageWarningFactKind, map[string]any{
		"cluster_id":     observation.Context.ClusterID,
		"generation":     observation.Context.GenerationID,
		"resource_scope": resourceScope,
		"source_state":   sourceState,
		"warning_kind":   warningKind,
	})
	payload := kubernetesPayload(observation.Context)
	payload["warning_kind"] = warningKind
	payload["source_state"] = sourceState
	payload["resource_scope"] = resourceScope
	payload["error_class"] = strings.TrimSpace(observation.ErrorClass)
	payload["message"] = strings.TrimSpace(observation.Message)
	payload["attributes"] = cloneAnyMap(observation.Attributes)
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.SecretsIAMCoverageWarningFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

func validateKubernetesContext(ctx KubernetesContext) error {
	switch {
	case strings.TrimSpace(ctx.ClusterID) == "":
		return fmt.Errorf("kubernetes secrets iam observation requires cluster_id")
	case strings.TrimSpace(ctx.ScopeID) == "":
		return fmt.Errorf("kubernetes secrets iam observation requires scope_id")
	case strings.TrimSpace(ctx.GenerationID) == "":
		return fmt.Errorf("kubernetes secrets iam observation requires generation_id")
	case strings.TrimSpace(ctx.CollectorInstanceID) == "":
		return fmt.Errorf("kubernetes secrets iam observation requires collector_instance_id")
	case ctx.FencingToken <= 0:
		return fmt.Errorf("kubernetes secrets iam observation fencing_token must be positive")
	default:
		return nil
	}
}

func kubernetesEnvelopeContext(ctx KubernetesContext) EnvelopeContext {
	return EnvelopeContext{
		AccountID:           "kubernetes",
		Region:              "cluster",
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func kubernetesPayload(ctx KubernetesContext) map[string]any {
	return map[string]any{
		"cluster_id":               strings.TrimSpace(ctx.ClusterID),
		"provider":                 ProviderKubernetes,
		"collector_instance_id":    strings.TrimSpace(ctx.CollectorInstanceID),
		"redaction_policy_version": RedactionPolicyVersion,
	}
}

func serviceAccountJoinKey(ctx KubernetesContext, namespace, name string) (string, error) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return "", fmt.Errorf("kubernetes service account join requires namespace and name")
	}
	return fingerprintKubernetesParts("service_account_join", ctx.ClusterID, namespace, name), nil
}

func kubernetesWebIdentitySubject(namespace, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return ""
	}
	return "system:serviceaccount:" + namespace + ":" + name
}

func rbacRoleJoinKey(ctx KubernetesContext, kind, namespace, name string) string {
	return fingerprintKubernetesParts("rbac_role_join", ctx.ClusterID, kind, namespace, name)
}

func fingerprintKubernetesValue(kind, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fingerprintKubernetesParts(kind, value)
}

func fingerprintKubernetesParts(kind string, parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	return "sha256:" + facts.StableID("SecretsIAMKubernetesFingerprint", map[string]any{
		"kind":  strings.TrimSpace(kind),
		"parts": normalized,
	})
}

func rbacRulePayloads(rules []KubernetesRBACRuleSummary) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		output = append(output, map[string]any{
			"verbs":                     normalizeActionList(rule.Verbs),
			"api_groups":                normalizeKeyList(rule.APIGroups),
			"resources":                 normalizeKeyList(rule.Resources),
			"resource_name_count":       rule.ResourceNameCount,
			"resource_names_present":    rule.ResourceNamesPresent,
			"non_resource_url_count":    rule.NonResourceURLCount,
			"non_resource_urls_present": rule.NonResourceURLsPresent,
		})
	}
	return output
}

func rbacSubjectPayloads(ctx KubernetesContext, subjects []KubernetesRBACSubject) []map[string]any {
	if len(subjects) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(subjects))
	for _, subject := range subjects {
		item := map[string]any{
			"kind":                  strings.TrimSpace(subject.Kind),
			"api_group":             strings.TrimSpace(subject.APIGroup),
			"namespace_fingerprint": fingerprintKubernetesValue("namespace", subject.Namespace),
			"name_fingerprint":      fingerprintKubernetesValue("rbac_subject", subject.Name),
		}
		if strings.TrimSpace(subject.Kind) == "ServiceAccount" && strings.TrimSpace(subject.Namespace) != "" {
			item["service_account_join_key"] = fingerprintKubernetesParts(
				"service_account_join",
				ctx.ClusterID,
				subject.Namespace,
				subject.Name,
			)
		}
		output = append(output, item)
	}
	return output
}

func normalizeBoolState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BoolStateTrue:
		return BoolStateTrue
	case BoolStateFalse:
		return BoolStateFalse
	default:
		return BoolStateUnknown
	}
}

func maxInt(first, second int) int {
	if first > second {
		return first
	}
	return second
}
