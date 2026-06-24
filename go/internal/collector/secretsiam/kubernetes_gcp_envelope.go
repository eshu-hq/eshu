// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewKubernetesGCPWorkloadIdentityBindingEnvelope builds the
// k8s_gcp_workload_identity_binding source fact for a ServiceAccount annotated
// with iam.gke.io/gcp-service-account under a configured GKE workload pool.
func NewKubernetesGCPWorkloadIdentityBindingEnvelope(
	observation KubernetesGCPWorkloadIdentityBindingObservation,
) (facts.Envelope, error) {
	if err := validateKubernetesContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	emailDigest := GCPServiceAccountEmailDigest(observation.GCPServiceAccountEmail)
	if emailDigest == "" {
		return facts.Envelope{}, fmt.Errorf("kubernetes GCP workload identity binding requires gcp service-account annotation")
	}
	workloadPool := strings.TrimSpace(observation.GCPWorkloadPool)
	if workloadPool == "" {
		return facts.Envelope{}, fmt.Errorf("kubernetes GCP workload identity binding requires gcp workload pool")
	}
	joinKey, err := serviceAccountJoinKey(
		observation.Context,
		observation.Namespace,
		observation.ServiceAccountName,
	)
	if err != nil {
		return facts.Envelope{}, err
	}
	subjectFingerprint := GCPWorkloadIdentitySubjectFingerprint(
		workloadPool,
		observation.Namespace,
		observation.ServiceAccountName,
	)
	if subjectFingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("kubernetes GCP workload identity binding requires subject identity")
	}
	stableKey := facts.StableID(facts.KubernetesGCPWorkloadIdentityBindingFactKind, map[string]any{
		"cluster_id":                                observation.Context.ClusterID,
		"gcp_service_account_email_digest":          emailDigest,
		"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
		"service_account_join_key":                  joinKey,
		"service_account_uid_fingerprint":           fingerprintKubernetesValue("uid", observation.ServiceAccountUID),
	})
	payload := kubernetesPayload(observation.Context)
	payload["namespace_fingerprint"] = fingerprintKubernetesValue("namespace", observation.Namespace)
	payload["service_account_fingerprint"] = fingerprintKubernetesValue("service_account", observation.ServiceAccountName)
	payload["service_account_join_key"] = joinKey
	payload["service_account_uid_fingerprint"] = fingerprintKubernetesValue("uid", observation.ServiceAccountUID)
	payload["gcp_service_account_email_digest"] = emailDigest
	payload["gcp_workload_identity_pool_fingerprint"] = fingerprintKubernetesValue("gcp_workload_pool", workloadPool)
	payload["gcp_workload_identity_subject_fingerprint"] = subjectFingerprint
	payload["annotation_present"] = observation.AnnotationPresent
	return newEnvelope(
		kubernetesEnvelopeContext(observation.Context),
		facts.KubernetesGCPWorkloadIdentityBindingFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}
