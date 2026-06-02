package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewVaultAuthMountEnvelope builds a vault_auth_mount source fact.
func NewVaultAuthMountEnvelope(observation VaultAuthMountObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	authMethod := strings.TrimSpace(observation.AuthMethod)
	if authMethod == "" {
		return facts.Envelope{}, fmt.Errorf("vault auth mount observation requires auth_method")
	}
	stableKey := facts.StableID(facts.VaultAuthMountFactKind, map[string]any{
		"mount_join_key":   mountKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["auth_method"] = authMethod
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultValue("mount_path", observation.MountPath)
	payload["mount_path_depth"] = vaultPathDepth(observation.MountPath)
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue("mount_accessor", observation.MountAccessor)
	payload["local"] = observation.Local
	payload["default_lease_ttl_seconds"] = observation.DefaultLeaseTTLSeconds
	payload["max_lease_ttl_seconds"] = observation.MaxLeaseTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultAuthMountFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultAuthRoleEnvelope builds a vault_auth_role source fact.
func NewVaultAuthRoleEnvelope(observation VaultAuthRoleObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	roleKey, err := vaultRoleJoinKey(observation.Context, observation.MountPath, observation.RoleName)
	if err != nil {
		return facts.Envelope{}, err
	}
	mountKey, _ := vaultMountJoinKey(observation.Context, observation.MountPath)
	policyKeys := vaultPolicyJoinKeys(observation.Context, observation.TokenPolicyNames)
	stableKey := facts.StableID(facts.VaultAuthRoleFactKind, map[string]any{
		"role_join_key":    roleKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["auth_method"] = strings.TrimSpace(observation.AuthMethod)
	payload["mount_join_key"] = mountKey
	payload["role_join_key"] = roleKey
	payload["role_name_fingerprint"] = fingerprintVaultValue("auth_role", observation.RoleName)
	payload["bound_service_account_name_count"] = len(normalizeKeyList(observation.BoundServiceAccountNames))
	payload["bound_service_account_namespace_count"] = len(normalizeKeyList(observation.BoundServiceAccountNamespaces))
	payload["bound_service_account_name_fingerprints"] = fingerprintVaultValues("service_account", observation.BoundServiceAccountNames)
	payload["bound_service_account_namespace_fingerprints"] = fingerprintVaultValues("namespace", observation.BoundServiceAccountNamespaces)
	payload["token_policy_count"] = len(policyKeys)
	payload["token_policy_join_keys"] = policyKeys
	payload["token_policy_name_fingerprints"] = fingerprintVaultValues("policy_name", observation.TokenPolicyNames)
	payload["token_ttl_seconds"] = observation.TokenTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultAuthRoleFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultACLPolicyEnvelope builds a vault_acl_policy source fact.
func NewVaultACLPolicyEnvelope(observation VaultACLPolicyObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	policyKey, err := vaultPolicyJoinKey(observation.Context, observation.PolicyName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultACLPolicyFactKind, map[string]any{
		"policy_join_key":  policyKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["policy_join_key"] = policyKey
	payload["policy_name_fingerprint"] = fingerprintVaultValue("policy_name", observation.PolicyName)
	payload["policy_hash_fingerprint"] = fingerprintVaultValue("policy_hash", observation.PolicyHash)
	payload["rules"] = vaultPolicyRulePayloads(observation.Rules)
	payload["rule_count"] = len(observation.Rules)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultACLPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultIdentityEntityEnvelope builds a vault_identity_entity source fact.
func NewVaultIdentityEntityEnvelope(observation VaultIdentityEntityObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	entityKey, err := vaultEntityJoinKey(observation.Context, observation.EntityID)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultIdentityEntityFactKind, map[string]any{
		"entity_join_key":  entityKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["entity_join_key"] = entityKey
	payload["entity_id_fingerprint"] = fingerprintVaultValue("entity_id", observation.EntityID)
	payload["entity_name_fingerprint"] = fingerprintVaultValue("entity_name", observation.EntityName)
	payload["alias_count"] = observation.AliasCount
	payload["group_count"] = observation.GroupCount
	payload["disabled"] = observation.Disabled
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultIdentityEntityFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultIdentityAliasEnvelope builds a vault_identity_alias source fact.
func NewVaultIdentityAliasEnvelope(observation VaultIdentityAliasObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	aliasID := strings.TrimSpace(observation.AliasID)
	if aliasID == "" {
		return facts.Envelope{}, fmt.Errorf("vault identity alias observation requires alias_id")
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	entityKey, err := vaultEntityJoinKey(observation.Context, observation.EntityID)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultIdentityAliasFactKind, map[string]any{
		"alias_id":         fingerprintVaultValue("alias_id", aliasID),
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["alias_id_fingerprint"] = fingerprintVaultValue("alias_id", aliasID)
	payload["alias_name_fingerprint"] = fingerprintVaultValue("alias_name", observation.AliasName)
	payload["entity_join_key"] = entityKey
	payload["mount_join_key"] = mountKey
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue("mount_accessor", observation.MountAccessor)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultIdentityAliasFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultKVMetadataEnvelope builds a vault_kv_metadata source fact.
func NewVaultKVMetadataEnvelope(observation VaultKVMetadataObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	pathFingerprint := fingerprintVaultPath(observation.Path)
	if pathFingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("vault kv metadata observation requires path")
	}
	stableKey := facts.StableID(facts.VaultKVMetadataFactKind, map[string]any{
		"kv_path_fingerprint": pathFingerprint,
		"mount_join_key":      mountKey,
		"vault_cluster_id":    observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultValue("mount_path", observation.MountPath)
	payload["kv_path_fingerprint"] = pathFingerprint
	payload["path_depth"] = vaultPathDepth(observation.Path)
	payload["current_version"] = observation.CurrentVersion
	payload["oldest_version"] = observation.OldestVersion
	payload["max_versions"] = observation.MaxVersions
	payload["cas_required"] = observation.CASRequired
	payload["delete_version_after_seconds"] = observation.DeleteVersionAfterSecs
	payload["custom_metadata_key_count"] = len(normalizeKeyList(observation.CustomMetadataKeys))
	payload["custom_metadata_key_fingerprints"] = fingerprintVaultValues("custom_metadata_key", observation.CustomMetadataKeys)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultKVMetadataFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultSecretEngineMountEnvelope builds a vault_secret_engine_mount source
// fact.
func NewVaultSecretEngineMountEnvelope(observation VaultSecretEngineMountObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	mountType := strings.TrimSpace(observation.MountType)
	if mountType == "" {
		return facts.Envelope{}, fmt.Errorf("vault secret engine mount observation requires mount_type")
	}
	stableKey := facts.StableID(facts.VaultSecretEngineMountFactKind, map[string]any{
		"mount_join_key":   mountKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultValue("mount_path", observation.MountPath)
	payload["mount_path_depth"] = vaultPathDepth(observation.MountPath)
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue("mount_accessor", observation.MountAccessor)
	payload["mount_type"] = mountType
	payload["kv_version"] = strings.TrimSpace(observation.KVVersion)
	payload["local"] = observation.Local
	payload["default_lease_ttl_seconds"] = observation.DefaultLeaseTTLSeconds
	payload["max_lease_ttl_seconds"] = observation.MaxLeaseTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultSecretEngineMountFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultCoverageWarningEnvelope builds secrets_iam_coverage_warning evidence
// for partial, hidden, unsupported, rate-limited, or stale Vault source reads.
func NewVaultCoverageWarningEnvelope(observation VaultCoverageWarningObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	sourceState := strings.TrimSpace(observation.SourceState)
	if warningKind == "" || sourceState == "" {
		return facts.Envelope{}, fmt.Errorf("vault coverage warning requires warning_kind and source_state")
	}
	resourceScope := strings.TrimSpace(observation.ResourceScope)
	stableKey := facts.StableID(facts.SecretsIAMCoverageWarningFactKind, map[string]any{
		"generation":       observation.Context.GenerationID,
		"resource_scope":   resourceScope,
		"source_state":     sourceState,
		"vault_cluster_id": observation.Context.VaultClusterID,
		"warning_kind":     warningKind,
	})
	payload := vaultPayload(observation.Context)
	payload["warning_kind"] = warningKind
	payload["source_state"] = sourceState
	payload["resource_scope"] = resourceScope
	payload["error_class"] = strings.TrimSpace(observation.ErrorClass)
	payload["message_present"] = strings.TrimSpace(observation.Message) != ""
	payload["message_fingerprint"] = fingerprintVaultValue("warning_message", observation.Message)
	payload["attribute_count"] = len(observation.Attributes)
	payload["attribute_key_fingerprints"] = fingerprintVaultValues("attribute_key", mapKeys(observation.Attributes))
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.SecretsIAMCoverageWarningFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

func validateVaultContext(ctx VaultContext) error {
	switch {
	case strings.TrimSpace(ctx.VaultClusterID) == "":
		return fmt.Errorf("vault secrets iam observation requires vault_cluster_id")
	case strings.TrimSpace(ctx.ScopeID) == "":
		return fmt.Errorf("vault secrets iam observation requires scope_id")
	case strings.TrimSpace(ctx.GenerationID) == "":
		return fmt.Errorf("vault secrets iam observation requires generation_id")
	case strings.TrimSpace(ctx.CollectorInstanceID) == "":
		return fmt.Errorf("vault secrets iam observation requires collector_instance_id")
	case ctx.FencingToken <= 0:
		return fmt.Errorf("vault secrets iam observation fencing_token must be positive")
	default:
		return nil
	}
}

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
		"namespace_fingerprint":    fingerprintVaultValue("namespace", ctx.Namespace),
		"namespace_depth":          vaultPathDepth(ctx.Namespace),
		"provider":                 ProviderVault,
		"collector_instance_id":    strings.TrimSpace(ctx.CollectorInstanceID),
		"redaction_policy_version": RedactionPolicyVersion,
	}
}

func vaultMountJoinKey(ctx VaultContext, mountPath string) (string, error) {
	mountPath = strings.TrimSpace(mountPath)
	if mountPath == "" {
		return "", fmt.Errorf("vault mount join requires mount_path")
	}
	return fingerprintVaultParts("mount_join", ctx.VaultClusterID, ctx.Namespace, mountPath), nil
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
	return fingerprintVaultParts("auth_role_join", mountKey, roleName), nil
}

func vaultPolicyJoinKey(ctx VaultContext, policyName string) (string, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return "", fmt.Errorf("vault policy join requires policy_name")
	}
	return fingerprintVaultParts("policy_join", ctx.VaultClusterID, ctx.Namespace, policyName), nil
}

func vaultEntityJoinKey(ctx VaultContext, entityID string) (string, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return "", fmt.Errorf("vault identity entity join requires entity_id")
	}
	return fingerprintVaultParts("entity_join", ctx.VaultClusterID, ctx.Namespace, entityID), nil
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

func vaultPolicyRulePayloads(rules []VaultACLPolicyRuleSummary) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		output = append(output, map[string]any{
			"path_fingerprint": fingerprintVaultPath(rule.Path),
			"path_depth":       vaultPathDepth(rule.Path),
			"capabilities":     normalizeActionList(rule.Capabilities),
		})
	}
	return output
}

func fingerprintVaultValues(kind string, values []string) []string {
	values = normalizeKeyList(values)
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, fingerprintVaultValue(kind, value))
	}
	return output
}

func fingerprintVaultPath(path string) string {
	return fingerprintVaultValue("path", strings.Join(vaultPathSegments(path), "/"))
}

func fingerprintVaultValue(kind, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fingerprintVaultParts(kind, value)
}

func fingerprintVaultParts(kind string, parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	return "sha256:" + facts.StableID("SecretsIAMVaultFingerprint", map[string]any{
		"kind":  strings.TrimSpace(kind),
		"parts": normalized,
	})
}

func vaultPathDepth(path string) int {
	return len(vaultPathSegments(path))
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
