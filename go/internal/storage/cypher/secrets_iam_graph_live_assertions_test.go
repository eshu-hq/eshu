// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"strings"
)

func countSuspiciousSecretsIAMLiveValues(
	ctx context.Context,
	exec liveSecretsIAMExecutor,
	scope string,
	evidence string,
) (int, error) {
	queries := []string{
		`MATCH (n:SecretsIAMServiceAccount {scope_id: $scope, evidence_source: $evidence_source})
RETURN n.uid, n.scope_id, n.generation_id, n.evidence_source, n.confidence`,
		`MATCH (n:SecretsIAMVaultAuthRole {scope_id: $scope, evidence_source: $evidence_source})
RETURN n.uid, n.vault_mount_join_key, n.scope_id, n.generation_id, n.evidence_source, n.confidence`,
		`MATCH (n:SecretsIAMVaultPolicy {scope_id: $scope, evidence_source: $evidence_source})
RETURN n.uid, n.scope_id, n.generation_id, n.evidence_source, n.confidence`,
		`MATCH (n:SecretsIAMSecretMetadataPath {scope_id: $scope, evidence_source: $evidence_source})
RETURN n.uid, n.vault_mount_join_key, n.kv_path_fingerprint, n.scope_id, n.generation_id, n.evidence_source, n.confidence`,
		`MATCH ()-[r:SECRETS_IAM_USES_SERVICE_ACCOUNT]->() WHERE r.scope_id = $scope AND r.evidence_source = $evidence_source
RETURN r.scope_id, r.generation_id, r.evidence_source, r.confidence, r.evidence_fact_ids`,
		`MATCH ()-[r:SECRETS_IAM_ASSUMES_IAM_ROLE]->() WHERE r.scope_id = $scope AND r.evidence_source = $evidence_source
RETURN r.assume_mode, r.scope_id, r.generation_id, r.evidence_source, r.confidence, r.evidence_fact_ids`,
		`MATCH ()-[r:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->() WHERE r.scope_id = $scope AND r.evidence_source = $evidence_source
RETURN r.scope_id, r.generation_id, r.evidence_source, r.confidence, r.evidence_fact_ids`,
		`MATCH ()-[r:SECRETS_IAM_USES_VAULT_POLICY]->() WHERE r.scope_id = $scope AND r.evidence_source = $evidence_source
RETURN r.scope_id, r.generation_id, r.evidence_source, r.confidence, r.evidence_fact_ids`,
		`MATCH ()-[r:SECRETS_IAM_GRANTS_SECRET_READ]->() WHERE r.scope_id = $scope AND r.evidence_source = $evidence_source
RETURN r.capabilities, r.scope_id, r.generation_id, r.evidence_source, r.confidence, r.evidence_fact_ids`,
	}
	params := map[string]any{"scope": scope, "evidence_source": evidence}
	suspicious := 0
	for _, query := range queries {
		values, err := exec.values(ctx, query, params)
		if err != nil {
			return 0, err
		}
		suspicious += countSuspiciousLiveValues(values)
	}
	return suspicious, nil
}

func countSuspiciousLiveValues(values []any) int {
	forbidden := []string{"arn:", "secret/data", "secret-value", "vault/path", "-----begin"}
	count := 0
	for _, value := range values {
		count += countSuspiciousLiveValue(value, forbidden)
	}
	return count
}

func countSuspiciousLiveValue(value any, forbidden []string) int {
	switch typed := value.(type) {
	case string:
		lower := strings.ToLower(typed)
		for _, marker := range forbidden {
			if strings.Contains(lower, marker) {
				return 1
			}
		}
	case []any:
		count := 0
		for _, item := range typed {
			count += countSuspiciousLiveValue(item, forbidden)
		}
		return count
	case []string:
		count := 0
		for _, item := range typed {
			count += countSuspiciousLiveValue(item, forbidden)
		}
		return count
	}
	return 0
}
