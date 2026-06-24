// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// secretManagerSecretAssetType is the Cloud Asset Inventory asset type for a
// Secret Manager secret. A permission grant whose bound resource is a secret is
// flagged so the reducer can surface Secret Manager resource-grant posture.
const secretManagerSecretAssetType = "secretmanager.googleapis.com/Secret"

// serviceAccountAssetType is the Cloud Asset Inventory asset type for a GCP IAM
// service account. Trust bindings on this resource become
// gcp_iam_trust_policy facts.
const serviceAccountAssetType = "iam.googleapis.com/ServiceAccount"

// gcpBroadRoles is the bounded set of primitive roles whose grant to a service
// account is broad-privilege posture evidence. Predefined fine-grained roles are
// not flagged broad; the reducer applies its own posture rules on top.
var gcpBroadRoles = map[string]struct{}{
	"roles/owner":  {},
	"roles/editor": {},
}

var gcpServiceAccountImpersonationRoles = map[string]string{
	"roles/iam.serviceAccountTokenCreator": secretsiam.GCPImpersonationModeTokenCreator,
	"roles/iam.serviceAccountUser":         secretsiam.GCPImpersonationModeServiceAccountUser,
	"roles/iam.workloadIdentityUser":       secretsiam.GCPImpersonationModeWorkloadIdentity,
}

// secretsIAMEnvelopes projects the generation's Cloud Asset Inventory IAM
// bindings into the GCP secrets/IAM source-fact mirror (issue #2347): one
// gcp_iam_principal per distinct service-account grantee (deduplicated by the
// redaction-safe member fingerprint) and one gcp_iam_permission_policy per
// (service-account member, role, resource) grant. Only serviceAccount-class
// members are admitted: human/group/public members are not identities that form
// trust chains. The member fingerprint is computed with the same scheme the
// gcp_iam_policy_observation members carry, so the reducer joins grants to
// principals and to the binding observation by construction. No raw member
// identity, email, or policy JSON is emitted.
func (g *Generation) secretsIAMEnvelopes() ([]facts.Envelope, error) {
	if g.key.IsZero() {
		return nil, nil
	}

	keys := make([]string, 0, len(g.resources))
	for key := range g.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	principals := make(map[string]facts.Envelope)
	var permissions []facts.Envelope
	var trusts []facts.Envelope
	for _, key := range keys {
		obs := g.resources[key]
		resourceName := strings.TrimSpace(obs.Name)
		// project_id is best-effort provenance parsed from the resource name; an
		// organization- or folder-level binding legitimately has none. It is never
		// fabricated from the Eshu scope id, which is not a GCP project.
		projectID := strings.TrimSpace(ProjectIDFromFullName(resourceName))
		resourceIsSecret := strings.TrimSpace(obs.AssetType) == secretManagerSecretAssetType
		ctx := g.gcpSecretsIAMContext(projectID, obs.SourceURI)

		for _, binding := range obs.IAMPolicyBindings {
			if !hasUsableIAMPolicyBinding(binding) {
				continue
			}
			role := strings.TrimSpace(binding.Role)
			conditionFingerprint := fingerprintIAMCondition(binding.ConditionFingerprintInput, g.key)
			if gcpTrustModeForBinding(obs, role) != "" {
				trustEnvelopes, err := g.gcpTrustPolicyEnvelopes(obs, binding, ctx, conditionFingerprint)
				if err != nil {
					return nil, err
				}
				trusts = append(trusts, trustEnvelopes...)
				continue
			}
			_, broadRole := gcpBroadRoles[role]
			for _, member := range binding.Members {
				if MemberClass(member) != secretsiam.GCPMemberClassServiceAccount {
					continue
				}
				fingerprint := FingerprintMember(member, g.key)
				if _, seen := principals[fingerprint]; !seen {
					principalEnv, err := secretsiam.NewGCPPrincipalEnvelope(secretsiam.GCPPrincipalObservation{
						Context:              ctx,
						PrincipalFingerprint: fingerprint,
						MemberClass:          secretsiam.GCPMemberClassServiceAccount,
					})
					if err != nil {
						return nil, err
					}
					principals[fingerprint] = principalEnv
				}
				permissionEnv, err := secretsiam.NewGCPPermissionPolicyEnvelope(secretsiam.GCPPermissionPolicyObservation{
					Context:              ctx,
					PrincipalFingerprint: fingerprint,
					Role:                 role,
					ResourceFullName:     resourceName,
					ResourceAssetType:    strings.TrimSpace(obs.AssetType),
					ResourceIsSecret:     resourceIsSecret,
					BroadRole:            broadRole,
					ConditionPresent:     binding.ConditionPresent,
					ConditionFingerprint: conditionFingerprint,
				})
				if err != nil {
					return nil, err
				}
				permissions = append(permissions, permissionEnv)
			}
		}
	}

	if len(principals) == 0 && len(permissions) == 0 && len(trusts) == 0 {
		return nil, nil
	}

	envelopes := make([]facts.Envelope, 0, len(principals)+len(permissions)+len(trusts))
	principalFingerprints := make([]string, 0, len(principals))
	for fingerprint := range principals {
		principalFingerprints = append(principalFingerprints, fingerprint)
	}
	sort.Strings(principalFingerprints)
	for _, fingerprint := range principalFingerprints {
		envelopes = append(envelopes, principals[fingerprint])
	}
	sort.Slice(permissions, func(i, j int) bool {
		return permissions[i].StableFactKey < permissions[j].StableFactKey
	})
	envelopes = append(envelopes, permissions...)
	sort.Slice(trusts, func(i, j int) bool {
		return trusts[i].StableFactKey < trusts[j].StableFactKey
	})
	envelopes = append(envelopes, trusts...)
	return envelopes, nil
}

func (g *Generation) gcpTrustPolicyEnvelopes(
	obs ResourceObservation,
	binding IAMPolicyBindingObservation,
	ctx secretsiam.GCPEnvelopeContext,
	conditionFingerprint string,
) ([]facts.Envelope, error) {
	targetEmail := gcpServiceAccountEmailForResource(obs)
	if targetEmail == "" {
		return nil, nil
	}
	role := strings.TrimSpace(binding.Role)
	mode := gcpTrustModeForBinding(obs, role)
	if mode == "" {
		return nil, nil
	}
	targetFingerprint := FingerprintMember("serviceAccount:"+targetEmail, g.key)
	emailDigest := secretsiam.GCPServiceAccountEmailDigest(targetEmail)
	cloudResourceUID := gcpCloudResourceUID(
		strings.TrimSpace(ProjectIDFromFullName(obs.Name)),
		strings.TrimSpace(obs.Location),
		strings.TrimSpace(obs.AssetType),
		strings.TrimSpace(obs.Name),
	)

	envelopes := make([]facts.Envelope, 0, len(binding.Members))
	for _, member := range binding.Members {
		if strings.TrimSpace(member) == "" {
			continue
		}
		trustedFingerprint := FingerprintMember(member, g.key)
		memberClass := MemberClass(member)
		pool, namespace, name, workloadIdentityMember := parseGCPWorkloadIdentityMember(member)
		workloadSubject := ""
		workloadClass := ""
		if workloadIdentityMember {
			workloadSubject = secretsiam.GCPWorkloadIdentitySubjectFingerprint(pool, namespace, name)
			workloadClass = secretsiam.GCPWorkloadIdentityMemberClassServiceAccount
		}
		env, err := secretsiam.NewGCPTrustPolicyEnvelope(secretsiam.GCPTrustPolicyObservation{
			Context:                               ctx,
			TargetPrincipalFingerprint:            targetFingerprint,
			TargetServiceAccountEmailDigest:       emailDigest,
			TargetServiceAccountCloudResourceUID:  cloudResourceUID,
			TrustedMemberFingerprint:              trustedFingerprint,
			TrustedMemberClass:                    memberClass,
			Role:                                  role,
			ImpersonationMode:                     mode,
			GCPWorkloadIdentitySubjectFingerprint: workloadSubject,
			GCPWorkloadIdentityMemberClass:        workloadClass,
			ConditionPresent:                      binding.ConditionPresent,
			ConditionFingerprint:                  conditionFingerprint,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	return envelopes, nil
}

func gcpTrustModeForBinding(obs ResourceObservation, role string) string {
	if strings.TrimSpace(obs.AssetType) != serviceAccountAssetType {
		return ""
	}
	return gcpServiceAccountImpersonationRoles[strings.TrimSpace(role)]
}

func gcpServiceAccountEmailForResource(obs ResourceObservation) string {
	if strings.TrimSpace(obs.AssetType) != serviceAccountAssetType {
		return ""
	}
	if email := strings.ToLower(strings.TrimSpace(obs.ServiceAccountEmail)); email != "" {
		return email
	}
	const marker = "/serviceAccounts/"
	name := strings.TrimSpace(obs.Name)
	index := strings.LastIndex(name, marker)
	if index < 0 {
		return ""
	}
	email := strings.TrimSpace(name[index+len(marker):])
	if !strings.Contains(email, "@") {
		return ""
	}
	return strings.ToLower(email)
}

func parseGCPWorkloadIdentityMember(member string) (string, string, string, bool) {
	const prefix = "serviceAccount:"
	trimmed := strings.TrimSpace(member)
	if !strings.HasPrefix(trimmed, prefix) {
		return "", "", "", false
	}
	value := strings.TrimPrefix(trimmed, prefix)
	open := strings.Index(value, "[")
	close := strings.LastIndex(value, "]")
	if open <= 0 || close <= open+1 || close != len(value)-1 {
		return "", "", "", false
	}
	pool := strings.TrimSpace(value[:open])
	subject := strings.TrimSpace(value[open+1 : close])
	parts := strings.SplitN(subject, "/", 2)
	if pool == "" || len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", "", false
	}
	return pool, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func gcpCloudResourceUID(projectID, location, assetType, fullResourceName string) string {
	return facts.StableID("CloudResource", map[string]any{
		"account_id":    strings.TrimSpace(projectID),
		"region":        strings.TrimSpace(location),
		"resource_id":   strings.TrimSpace(fullResourceName),
		"resource_type": strings.TrimSpace(assetType),
	})
}

// gcpSecretsIAMContext builds the GCP secrets/IAM envelope context for one
// observation project from the generation boundary.
func (g *Generation) gcpSecretsIAMContext(projectID, sourceURI string) secretsiam.GCPEnvelopeContext {
	return secretsiam.GCPEnvelopeContext{
		ProjectID:           projectID,
		LocationBucket:      g.boundary.LocationBucket,
		ScopeID:             g.boundary.ScopeID,
		GenerationID:        g.boundary.GenerationID,
		CollectorInstanceID: g.boundary.CollectorInstanceID,
		FencingToken:        g.boundary.FencingToken,
		ObservedAt:          g.boundary.ObservedAt,
		SourceURI:           strings.TrimSpace(sourceURI),
	}
}
