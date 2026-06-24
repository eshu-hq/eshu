// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const (
	secretsIAMIdentityTrustChainFactKind          = "reducer_secrets_iam_identity_trust_chain"
	secretsIAMPrivilegePostureObservationFactKind = "reducer_secrets_iam_privilege_posture_observation"
	secretsIAMSecretAccessPathFactKind            = "reducer_secrets_iam_secret_access_path"
	secretsIAMPostureGapFactKind                  = "reducer_secrets_iam_posture_gap"
)

// PostgresSecretsIAMTrustChainWriter stores secrets/IAM reducer read models in
// the shared fact store. It adds no table, graph label, graph edge, or DDL.
type PostgresSecretsIAMTrustChainWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteSecretsIAMTrustChainReadModels persists all secrets/IAM read-model
// outputs with retry-idempotent fact identities.
func (w PostgresSecretsIAMTrustChainWriter) WriteSecretsIAMTrustChainReadModels(
	ctx context.Context,
	write SecretsIAMTrustChainWrite,
) (SecretsIAMTrustChainWriteResult, error) {
	if w.DB == nil {
		return SecretsIAMTrustChainWriteResult{}, fmt.Errorf("secrets/IAM trust-chain database is required")
	}
	now := reducerWriterNow(w.Now)
	factsWritten := 0
	for _, chain := range write.Models.IdentityTrustChains {
		if err := w.writePayload(ctx, now, write, secretsIAMIdentityTrustChainFactKind, chain.ChainID, secretsIAMIdentityTrustChainPayload(write, chain)); err != nil {
			return SecretsIAMTrustChainWriteResult{}, err
		}
		factsWritten++
	}
	for _, observation := range write.Models.PrivilegePostureObservations {
		if err := w.writePayload(ctx, now, write, secretsIAMPrivilegePostureObservationFactKind, observation.ObservationID, secretsIAMPrivilegePostureObservationPayload(write, observation)); err != nil {
			return SecretsIAMTrustChainWriteResult{}, err
		}
		factsWritten++
	}
	for _, path := range write.Models.SecretAccessPaths {
		if err := w.writePayload(ctx, now, write, secretsIAMSecretAccessPathFactKind, path.PathID, secretsIAMSecretAccessPathPayload(write, path)); err != nil {
			return SecretsIAMTrustChainWriteResult{}, err
		}
		factsWritten++
	}
	for _, gap := range write.Models.PostureGaps {
		if err := w.writePayload(ctx, now, write, secretsIAMPostureGapFactKind, gap.GapID, secretsIAMPostureGapPayload(write, gap)); err != nil {
			return SecretsIAMTrustChainWriteResult{}, err
		}
		factsWritten++
	}
	return SecretsIAMTrustChainWriteResult{
		FactsWritten:    factsWritten,
		EvidenceSummary: fmt.Sprintf("wrote secrets/IAM trust-chain read-model facts=%d", factsWritten),
	}, nil
}

func (w PostgresSecretsIAMTrustChainWriter) writePayload(
	ctx context.Context,
	now time.Time,
	write SecretsIAMTrustChainWrite,
	factKind string,
	modelID string,
	payload map[string]any,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal secrets/IAM trust-chain payload: %w", err)
	}
	if _, err := w.DB.ExecContext(
		ctx,
		canonicalReducerFactInsertQuery,
		secretsIAMReadModelFactID(write, factKind, modelID),
		write.ScopeID,
		write.GenerationID,
		factKind,
		secretsIAMReadModelStableFactKey(write, factKind, modelID),
		reducerFactCollectorKind(write.SourceSystem),
		facts.SourceConfidenceInferred,
		write.SourceSystem,
		write.IntentID,
		nil,
		nil,
		now,
		now,
		false,
		payloadJSON,
	); err != nil {
		return fmt.Errorf("write secrets/IAM trust-chain fact: %w", err)
	}
	return nil
}

func secretsIAMReadModelFactID(write SecretsIAMTrustChainWrite, factKind, modelID string) string {
	return factKind + ":" + facts.StableID(factKind, map[string]any{
		"identity": secretsIAMReadModelIdentity(write, factKind, modelID),
	})
}

func secretsIAMReadModelStableFactKey(write SecretsIAMTrustChainWrite, factKind, modelID string) string {
	return factKind + ":" + facts.StableID("SecretsIAMReadModelStableFactKey", map[string]any{
		"identity": secretsIAMReadModelIdentity(write, factKind, modelID),
	})
}

func secretsIAMReadModelIdentity(write SecretsIAMTrustChainWrite, factKind, modelID string) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"fact_kind":     strings.TrimSpace(factKind),
		"model_id":      strings.TrimSpace(modelID),
	}
}

func secretsIAMBasePayload(write SecretsIAMTrustChainWrite, modelKind string) map[string]any {
	return map[string]any{
		"reducer_domain":    string(DomainSecretsIAMTrustChain),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"model_kind":        modelKind,
		"seed_fact_count":   write.LoadStats.SeedFactCount,
		"loaded_fact_count": write.LoadStats.LoadedFactCount,
		"load_truncated":    write.LoadStats.Truncated,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}

func secretsIAMIdentityTrustChainPayload(
	write SecretsIAMTrustChainWrite,
	chain SecretsIAMIdentityTrustChain,
) map[string]any {
	payload := secretsIAMBasePayload(write, "identity_trust_chain")
	payload["chain_id"] = chain.ChainID
	payload["state"] = string(chain.State)
	payload["confidence"] = chain.Confidence
	payload["service_account_join_key"] = chain.ServiceAccountJoinKey
	payload["workload_object_id"] = chain.WorkloadObjectID
	payload["workload_kind"] = chain.WorkloadKind
	payload["iam_role_fingerprint"] = chain.IAMRoleFingerprint
	payload["iam_role_cloud_resource_uid"] = chain.IAMRoleCloudResourceUID
	payload["iam_role_assume_mode"] = chain.IAMRoleAssumeMode
	payload["gcp_service_account_fingerprint"] = chain.GCPServiceAccountFingerprint
	payload["gcp_service_account_cloud_resource_uid"] = chain.GCPServiceAccountCloudResourceUID
	payload["gcp_service_account_assume_mode"] = chain.GCPServiceAccountAssumeMode
	payload["vault_role_join_key"] = chain.VaultRoleJoinKey
	payload["vault_mount_join_key"] = chain.VaultMountJoinKey
	payload["vault_policy_join_keys"] = uniqueSortedStrings(chain.VaultPolicyJoinKeys)
	payload["evidence_fact_ids"] = uniqueSortedStrings(chain.EvidenceFactIDs)
	payload["missing_evidence"] = uniqueSortedStrings(chain.MissingEvidence)
	payload["source_scopes"] = uniqueSortedStrings(chain.SourceScopes)
	payload["source_generations"] = uniqueSortedStrings(chain.SourceGenerations)
	return payload
}

func secretsIAMPrivilegePostureObservationPayload(
	write SecretsIAMTrustChainWrite,
	observation SecretsIAMPrivilegePostureObservation,
) map[string]any {
	payload := secretsIAMBasePayload(write, "privilege_posture_observation")
	payload["observation_id"] = observation.ObservationID
	payload["risk_type"] = observation.RiskType
	payload["severity"] = observation.Severity
	payload["state"] = string(observation.State)
	payload["confidence"] = observation.Confidence
	payload["subject_fingerprint"] = observation.SubjectFingerprint
	payload["reason"] = observation.Reason
	payload["evidence_fact_ids"] = uniqueSortedStrings(observation.EvidenceFactIDs)
	return payload
}

func secretsIAMSecretAccessPathPayload(
	write SecretsIAMTrustChainWrite,
	path SecretsIAMSecretAccessPath,
) map[string]any {
	payload := secretsIAMBasePayload(write, "secret_access_path")
	payload["path_id"] = path.PathID
	payload["chain_id"] = path.ChainID
	payload["state"] = string(path.State)
	payload["confidence"] = path.Confidence
	payload["kv_path_fingerprint"] = path.KVPathFingerprint
	payload["vault_mount_join_key"] = path.VaultMountJoinKey
	payload["vault_policy_join_key"] = path.VaultPolicyJoinKey
	payload["cloud_provider"] = path.CloudProvider
	payload["cloud_secret_resource_fingerprint"] = path.CloudSecretResourceFingerprint
	payload["capabilities"] = uniqueSortedStrings(path.Capabilities)
	payload["evidence_fact_ids"] = uniqueSortedStrings(path.EvidenceFactIDs)
	return payload
}

func secretsIAMPostureGapPayload(
	write SecretsIAMTrustChainWrite,
	gap SecretsIAMPostureGap,
) map[string]any {
	payload := secretsIAMBasePayload(write, "posture_gap")
	payload["gap_id"] = gap.GapID
	payload["gap_type"] = gap.GapType
	payload["state"] = string(gap.State)
	payload["reason"] = gap.Reason
	payload["service_account_join_key"] = gap.ServiceAccountJoinKey
	payload["evidence_fact_ids"] = uniqueSortedStrings(gap.EvidenceFactIDs)
	payload["missing_evidence"] = uniqueSortedStrings(gap.MissingEvidence)
	payload["unsupported_layers"] = uniqueSortedStrings(gap.UnsupportedLayers)
	return payload
}
