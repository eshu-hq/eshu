// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func sortSecretsIAMReadModels(models *SecretsIAMTrustChainReadModels) {
	sort.Slice(models.IdentityTrustChains, func(i, j int) bool {
		return models.IdentityTrustChains[i].ChainID < models.IdentityTrustChains[j].ChainID
	})
	sort.Slice(models.PrivilegePostureObservations, func(i, j int) bool {
		return models.PrivilegePostureObservations[i].ObservationID < models.PrivilegePostureObservations[j].ObservationID
	})
	sort.Slice(models.SecretAccessPaths, func(i, j int) bool {
		return models.SecretAccessPaths[i].PathID < models.SecretAccessPaths[j].PathID
	})
	sort.Slice(models.PostureGaps, func(i, j int) bool {
		return models.PostureGaps[i].GapID < models.PostureGaps[j].GapID
	})
}

func secretsIAMID(kind string, parts ...string) string {
	return kind + ":" + facts.StableID(kind, map[string]any{"parts": parts})
}

func secretsIAMFingerprint(kind string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "sha256:" + facts.StableID("SecretsIAMReducerFingerprint", map[string]any{
		"kind":  kind,
		"value": value,
	})
}

func addByKey(index map[string][]facts.Envelope, key string, envelope facts.Envelope) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], envelope)
}

// addServiceAccountByKey indexes a decoded k8s_service_account fact by its
// join key. Like addByKey, it TrimSpaces the key and skips a blank one: the
// factschema seam treats a present-but-EMPTY required string as a valid decode
// (present-but-empty is valid), so a fact with service_account_join_key:""
// or "   " decodes successfully and would otherwise be indexed under a blank
// key — reintroducing the empty-identity gap (or a spurious exact chain when
// two blank-key facts arrive together) the pre-typing addByKey trim-and-skip
// prevented.
func addServiceAccountByKey(index map[string][]secretsIAMServiceAccount, key string, account secretsIAMServiceAccount) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], account)
}

// addWorkloadByKey mirrors addServiceAccountByKey for decoded
// k8s_workload_identity_use facts.
func addWorkloadByKey(index map[string][]secretsIAMWorkload, key string, workload secretsIAMWorkload) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], workload)
}

// addIRSAByKey mirrors addServiceAccountByKey for decoded
// eks_irsa_annotation/eks_pod_identity_association facts, which share one
// index keyed by ServiceAccountJoinKey.
func addIRSAByKey(index map[string][]secretsIAMIRSA, key string, irsa secretsIAMIRSA) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], irsa)
}

// addGCPBindingByKey mirrors addServiceAccountByKey for decoded
// k8s_gcp_workload_identity_binding facts.
func addGCPBindingByKey(index map[string][]secretsIAMGCPBinding, key string, binding secretsIAMGCPBinding) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], binding)
}

// addVaultRoleByKey mirrors addServiceAccountByKey for decoded
// vault_auth_role facts, keyed by each of their (already-validated, non-empty)
// bound service-account join keys.
func addVaultRoleByKey(index map[string][]secretsIAMVaultRole, key string, role secretsIAMVaultRole) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], role)
}

// addVaultPolicyByKey mirrors addServiceAccountByKey for decoded
// vault_acl_policy facts.
func addVaultPolicyByKey(index map[string][]secretsIAMVaultPolicy, key string, policy secretsIAMVaultPolicy) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], policy)
}

// addVaultKVByKey mirrors addServiceAccountByKey for decoded
// vault_kv_metadata facts.
func addVaultKVByKey(index map[string][]secretsIAMVaultKV, key string, kv secretsIAMVaultKV) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	index[key] = append(index[key], kv)
}

// stringOrEmpty dereferences an optional *string payload field, returning ""
// for a nil pointer. It centralizes the pointer-to-value conversion the
// trust-chain build needs for every optional typed field it reads, matching
// the tolerant zero-value behavior the pre-typing payloadString(...) lookup
// had for an absent key.
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// boolOrFalse dereferences an optional *bool payload field, returning false
// for a nil pointer, matching the pre-typing payloadBool(...) zero-value
// behavior for an absent key.
func boolOrFalse(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// serviceAccountFactIDs collects the FactIDs of a decoded
// secretsIAMServiceAccount slice, sorted and de-duplicated. It replaces the
// pre-typing factIDs([]facts.Envelope) helper now that
// index.serviceAccounts holds the decoded pair type
// buildSecretsIAMIndex stores.
func serviceAccountFactIDs(accounts []secretsIAMServiceAccount) []string {
	out := make([]string, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, account.env.FactID)
	}
	return uniqueSortedStrings(out)
}

func secretsIAMContainsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func secretsIAMContainsLower(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func secretsIAMStateFromSourceState(sourceState string) SecretsIAMTrustChainState {
	switch strings.TrimSpace(sourceState) {
	case string(SecretsIAMTrustChainStateUnsupported):
		return SecretsIAMTrustChainStateUnsupported
	case string(SecretsIAMTrustChainStatePermissionHidden):
		return SecretsIAMTrustChainStatePermissionHidden
	case string(SecretsIAMTrustChainStateStale):
		return SecretsIAMTrustChainStateStale
	case string(SecretsIAMTrustChainStatePartial):
		return SecretsIAMTrustChainStatePartial
	default:
		return SecretsIAMTrustChainStatePartial
	}
}
