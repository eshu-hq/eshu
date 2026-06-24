// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GCPServiceAccountEmailDigest returns the stable redaction-safe digest used to
// join a GKE annotation target to the GCP IAM ServiceAccount trust fact.
func GCPServiceAccountEmailDigest(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return ""
	}
	return "sha256:" + facts.StableID("SecretsIAMGCPServiceAccountEmail", map[string]any{
		"email": normalized,
	})
}

// GCPWorkloadIdentitySubjectFingerprint returns the redaction-safe join
// fingerprint for a GKE Workload Identity subject scoped by workload pool.
func GCPWorkloadIdentitySubjectFingerprint(pool, namespace, serviceAccount string) string {
	pool = strings.TrimSpace(pool)
	namespace = strings.TrimSpace(namespace)
	serviceAccount = strings.TrimSpace(serviceAccount)
	if pool == "" || namespace == "" || serviceAccount == "" {
		return ""
	}
	return "sha256:" + facts.StableID("SecretsIAMGCPWorkloadIdentitySubject", map[string]any{
		"pool":            pool,
		"namespace":       namespace,
		"service_account": serviceAccount,
	})
}
