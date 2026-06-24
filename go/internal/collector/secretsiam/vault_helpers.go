// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func vaultEnvelopeContext(ctx VaultContext) EnvelopeContext {
	return EnvelopeContext{
		AccountID:           "vault",
		Region:              "cluster",
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func vaultPayload(ctx VaultContext) map[string]any {
	return map[string]any{
		"vault_cluster_id":         strings.TrimSpace(ctx.VaultClusterID),
		"namespace_fingerprint":    fingerprintVaultValue(ctx, "namespace", ctx.Namespace),
		"namespace_depth":          vaultPathDepth(ctx.Namespace),
		"provider":                 ProviderVault,
		"collector_instance_id":    strings.TrimSpace(ctx.CollectorInstanceID),
		"redaction_policy_version": RedactionPolicyVersion,
	}
}

func vaultMountJoinKey(ctx VaultContext, mountPath string) (string, error) {
	mountPath = canonicalVaultPath(mountPath)
	if mountPath == "" {
		return "", fmt.Errorf("vault mount join requires mount_path")
	}
	return fingerprintVaultParts(ctx, "mount_join", ctx.VaultClusterID, ctx.Namespace, mountPath), nil
}

func vaultRoleJoinKey(ctx VaultContext, mountPath, roleName string) (string, error) {
	mountKey, err := vaultMountJoinKey(ctx, mountPath)
	if err != nil {
		return "", err
	}
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return "", fmt.Errorf("vault role join requires role_name")
	}
	return fingerprintVaultParts(ctx, "auth_role_join", mountKey, roleName), nil
}

func vaultPolicyJoinKey(ctx VaultContext, policyName string) (string, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return "", fmt.Errorf("vault policy join requires policy_name")
	}
	return fingerprintVaultParts(ctx, "policy_join", ctx.VaultClusterID, ctx.Namespace, policyName), nil
}

func vaultEntityJoinKey(ctx VaultContext, entityID string) (string, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return "", fmt.Errorf("vault identity entity join requires entity_id")
	}
	return fingerprintVaultParts(ctx, "entity_join", ctx.VaultClusterID, ctx.Namespace, entityID), nil
}

func vaultPolicyJoinKeys(ctx VaultContext, policyNames []string) []string {
	policyNames = normalizeKeyList(policyNames)
	output := make([]string, 0, len(policyNames))
	for _, policyName := range policyNames {
		key, err := vaultPolicyJoinKey(ctx, policyName)
		if err == nil {
			output = append(output, key)
		}
	}
	return output
}

func vaultBoundServiceAccountJoinKeys(
	ctx VaultContext,
	kubernetesClusterID string,
	namespaces []string,
	names []string,
) []string {
	kubernetesClusterID = strings.TrimSpace(kubernetesClusterID)
	if hasWildcard(namespaces) || hasWildcard(names) {
		return nil
	}
	namespaces = exactSelectorValues(namespaces)
	names = exactSelectorValues(names)
	if kubernetesClusterID == "" || len(namespaces) == 0 || len(names) == 0 {
		return nil
	}
	k8sCtx := KubernetesContext{ClusterID: kubernetesClusterID}
	joinKeys := make([]string, 0, len(namespaces)*len(names))
	for _, namespace := range namespaces {
		for _, name := range names {
			key, err := serviceAccountJoinKey(k8sCtx, namespace, name)
			if err == nil {
				joinKeys = append(joinKeys, key)
			}
		}
	}
	return normalizeKeyList(joinKeys)
}

func exactSelectorValues(values []string) []string {
	values = normalizeKeyList(values)
	output := make([]string, 0, len(values))
	for _, value := range values {
		if !hasWildcard([]string{value}) {
			output = append(output, value)
		}
	}
	return output
}

func hasWildcard(values []string) bool {
	for _, value := range values {
		if strings.ContainsAny(strings.TrimSpace(value), "*?[]") {
			return true
		}
	}
	return false
}

func vaultPolicyRulePayloads(ctx VaultContext, rules []VaultACLPolicyRuleSummary) []map[string]any {
	if len(rules) == 0 {
		return []map[string]any{}
	}
	output := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		output = append(output, map[string]any{
			"path_fingerprint": fingerprintVaultPath(ctx, rule.Path),
			"path_depth":       vaultPathDepth(rule.Path),
			"capabilities":     normalizeActionList(rule.Capabilities),
		})
	}
	return output
}

func fingerprintVaultValues(ctx VaultContext, kind string, values []string) []string {
	values = normalizeKeyList(values)
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, fingerprintVaultValue(ctx, kind, value))
	}
	return output
}

func fingerprintVaultPath(ctx VaultContext, path string) string {
	return fingerprintVaultValue(ctx, "path", canonicalVaultPath(path))
}

func fingerprintVaultMountPath(ctx VaultContext, path string) string {
	return fingerprintVaultValue(ctx, "mount_path", canonicalVaultPath(path))
}

func fingerprintVaultValue(ctx VaultContext, kind, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fingerprintVaultParts(ctx, kind, value)
}

func fingerprintVaultParts(ctx VaultContext, kind string, parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	rawIdentity := facts.StableID("SecretsIAMVaultFingerprint", map[string]any{
		"kind":  strings.TrimSpace(kind),
		"parts": normalized,
	})
	return redact.String(rawIdentity, "vault_metadata", strings.TrimSpace(kind), ctx.RedactionKey).Marker
}

func vaultPathDepth(path string) int {
	return len(vaultPathSegments(path))
}

func canonicalVaultPath(path string) string {
	return strings.Join(vaultPathSegments(path), "/")
}

func vaultPathSegments(path string) []string {
	parts := strings.FieldsFunc(strings.TrimSpace(path), func(r rune) bool {
		return r == '/'
	})
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

func mapKeys(input map[string]any) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	return keys
}
