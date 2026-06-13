package gcpcloud

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// secretManagerSecretAssetType is the Cloud Asset Inventory asset type for a
// Secret Manager secret. A permission grant whose bound resource is a secret is
// flagged so the reducer can treat it as secret-access posture.
const secretManagerSecretAssetType = "secretmanager.googleapis.com/Secret"

// gcpBroadRoles is the bounded set of primitive roles whose grant to a service
// account is broad-privilege posture evidence. Predefined fine-grained roles are
// not flagged broad; the reducer applies its own posture rules on top.
var gcpBroadRoles = map[string]struct{}{
	"roles/owner":  {},
	"roles/editor": {},
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

	if len(principals) == 0 && len(permissions) == 0 {
		return nil, nil
	}

	envelopes := make([]facts.Envelope, 0, len(principals)+len(permissions))
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
	return envelopes, nil
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
