// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// The uses_vault_policy family Odù (#6228 slice A, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to the
// secrets/IAM graph writer through the WriteUsesVaultPolicyEdges port of
// the DomainSecretsIAMGraphProjection handler. The relationship type is
// SECRETS_IAM_USES_VAULT_POLICY, read off
// SecretsIAMUsesVaultPolicyEdgeUpsertCypher's MERGE, never derived from
// the port or family name.
//
// Facts are built as typed secretsiamv1 / iamv1 values and encoded through
// the shared fixture seam, never as hand-built maps (Contract System v1).
// The aws_iam_trust_policy facts have no typed struct yet, so they stay raw
// maps read the way the reducer reads them -- that is the incremental
// migration, not a violation.
//
// Every fact shares one scope generation: the trust-chain handler loads one
// scope generation's source facts, so a fixture spanning scopes would not
// mirror any real intent. The scope is deliberately NOT any scope a sibling
// cassette owns: scopes carry one ACTIVE generation, so driving a second
// generation into an occupied scope supersedes the first family's
// generation (the iam_can_assume lesson).

const (
	// UsesVaultPolicyFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:uses_vault_policy coverage row names to resolve
	// through it.
	UsesVaultPolicyFamilyOduName = "odu:ifa-uses-vault-policy-family"

	// usesVaultPolicyFamilyScopeID is the single scope every fact in this
	// Odù belongs to.
	usesVaultPolicyFamilyScopeID = "vault:eshu-fixture-uses-account"

	// usesVaultPolicyFamilyGenerationID is the one scope generation the
	// Odù replays.
	usesVaultPolicyFamilyGenerationID = "gen-ifa-uses-vault-policy-family-1"
)

// The fixture join keys, fingerprints, and ARNs. Two roles carry token
// policies through both identity mechanisms (IRSA web-identity and EKS Pod
// Identity) and produce the three edges; every other lane is a deliberate
// non-producer proving the chain resolves to nothing where it must.
const (
	usesFamilyServiceAccountA = "sha256:sa-uses-app-a"
	usesFamilyServiceAccountB = "sha256:sa-uses-app-b"
	usesFamilyServiceAccountC = "sha256:sa-uses-app-c"
	usesFamilyServiceAccountD = "sha256:sa-uses-app-d"
	usesFamilyServiceAccountE = "sha256:sa-uses-app-e"
	usesFamilyServiceAccountF = "sha256:sa-uses-app-f"

	usesFamilyRoleARNA = "arn:aws:iam::123456789012:role/uses-app-a"
	usesFamilyRoleARNB = "arn:aws:iam::123456789012:role/uses-app-b"
	usesFamilyRoleARNC = "arn:aws:iam::123456789012:role/uses-app-c"
	usesFamilyRoleARND = "arn:aws:iam::123456789012:role/uses-app-d"
	usesFamilyRoleARNE = "arn:aws:iam::123456789012:role/uses-app-e"
	usesFamilyRoleARNF = "arn:aws:iam::123456789012:role/uses-app-f"

	usesFamilySubjectA = "sha256:uses-web-identity-subject-a"
	usesFamilySubjectC = "sha256:uses-web-identity-subject-c"
	usesFamilySubjectD = "sha256:uses-web-identity-subject-d"
	usesFamilySubjectF = "sha256:uses-web-identity-subject-f"

	usesFamilyVaultRole1 = "sha256:uses-vault-role-1"
	usesFamilyVaultRole2 = "sha256:uses-vault-role-2"
	usesFamilyVaultRole3 = "sha256:uses-vault-role-3"
	usesFamilyVaultRole4 = "sha256:uses-vault-role-4"
	usesFamilyVaultRole5 = "sha256:uses-vault-role-5"

	usesFamilyVaultPolicy1 = "sha256:uses-vault-policy-1"
	usesFamilyVaultPolicy2 = "sha256:uses-vault-policy-2"
	usesFamilyVaultPolicy3 = "sha256:uses-vault-policy-3"

	usesFamilyMount = "sha256:uses-vault-mount"
)

// usesVaultPolicyFamilyWorkloadID is the stable workload identifier every
// workload identity-use fact carries, suffixed per lane.
func usesVaultPolicyFamilyWorkloadID(lane string) string {
	return "wl-uses-" + lane
}

// UsesVaultPolicyFamilyOdu builds the cataloged Odù for the
// uses_vault_policy direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanAssumeFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
// appendUsesVaultPolicyVaultRole appends the Vault auth-role fact. policies
// lists the token policy join keys the role's issued tokens carry: the uses
// edges fan out one per key, so the two exact lanes carry policies
// and every other role carries none.
func appendUsesVaultPolicyVaultRole(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleKey string, bound []string, wildcard bool, policies []string) {
	mount := usesFamilyMount
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.VaultAuthRoleFactKind,
		scopeID, generationID, roleKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.VaultAuthRole{
			RoleJoinKey:                         roleKey,
			MountJoinKey:                        &mount,
			BoundServiceAccountJoinKeys:         bound,
			BoundServiceAccountSelectorWildcard: &wildcard,
			TokenPolicyJoinKeys:                 policies,
		}),
	))
}

func UsesVaultPolicyFamilyOdu() CatalogOdu {
	const oduName = UsesVaultPolicyFamilyOduName
	const scopeID = usesVaultPolicyFamilyScopeID
	const generationID = usesVaultPolicyFamilyGenerationID
	factsForOdu := make([]facts.Envelope, 0, 35)

	addServiceAccount := func(joinKey string) {
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.KubernetesServiceAccountFactKind,
			scopeID, generationID, joinKey,
			mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesServiceAccount{
				ServiceAccountJoinKey: joinKey,
			}),
		))
	}
	addWorkloadUse := func(joinKey, lane string) {
		workloadID := usesVaultPolicyFamilyWorkloadID(lane)
		workloadKind := "deployments"
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.KubernetesWorkloadIdentityUseFactKind,
			scopeID, generationID, joinKey,
			mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesWorkloadIdentityUse{
				ServiceAccountJoinKey: joinKey,
				WorkloadObjectID:      &workloadID,
				WorkloadKind:          &workloadKind,
			}),
		))
	}
	addIRSA := func(joinKey, roleARN, subject string) {
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.EKSIRSAAnnotationFactKind,
			scopeID, generationID, joinKey,
			mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSIRSAAnnotation{
				ServiceAccountJoinKey:         joinKey,
				RoleARN:                       roleARN,
				WebIdentitySubjectFingerprint: &subject,
			}),
		))
	}
	addPodIdentity := func(joinKey, roleARN string) {
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.EKSPodIdentityAssociationFactKind,
			scopeID, generationID, joinKey,
			mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSPodIdentityAssociation{
				ServiceAccountJoinKey: joinKey,
				RoleARN:               roleARN,
			}),
		))
	}
	addPrincipal := func(roleARN string) {
		provider := "aws_iam"
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.AWSIAMPrincipalFactKind,
			scopeID, generationID, roleARN,
			mustEncodeSecretsIAMFixturePayload(oduName, iamv1.Principal{
				AccountID:     "123456789012",
				Region:        "us-east-1",
				PrincipalARN:  roleARN,
				PrincipalType: "aws_iam_role",
				Provider:      &provider,
			}),
		))
	}
	// addWebIdentityTrust appends a raw-map aws_iam_trust_policy fact: the
	// kind has no typed struct yet, so the map mirrors exactly what the
	// reducer's exactWebIdentityTrust reads.
	addWebIdentityTrust := func(roleARN, subject, effect string, wildcard bool) {
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.AWSIAMTrustPolicyFactKind,
			scopeID, generationID, roleARN+":"+effect,
			map[string]any{
				"provider":                          "aws_iam",
				"role_arn":                          roleARN,
				"effect":                            effect,
				"actions":                           []string{"sts:assumerolewithwebidentity"},
				"web_identity_subject_fingerprints": []string{subject},
				"web_identity_subject_wildcard":     wildcard,
			},
		))
	}
	addPodIdentityTrust := func(roleARN string) {
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.AWSIAMTrustPolicyFactKind,
			scopeID, generationID, roleARN+":pod-identity",
			map[string]any{
				"provider":          "aws_iam",
				"role_arn":          roleARN,
				"effect":            "Allow",
				"actions":           []string{"sts:AssumeRole"},
				"assume_principals": []string{"pods.eks.amazonaws.com"},
			},
		))
	}
	// Lane A, EDGES: IRSA web-identity exact chain whose role carries two
	// token policies, fanning out to two uses edges.
	addServiceAccount(usesFamilyServiceAccountA)
	addWorkloadUse(usesFamilyServiceAccountA, "a")
	addIRSA(usesFamilyServiceAccountA, usesFamilyRoleARNA, usesFamilySubjectA)
	addPrincipal(usesFamilyRoleARNA)
	addWebIdentityTrust(usesFamilyRoleARNA, usesFamilySubjectA, "Allow", false)
	appendUsesVaultPolicyVaultRole(&factsForOdu, oduName, scopeID, generationID,
		usesFamilyVaultRole1, []string{usesFamilyServiceAccountA}, false,
		[]string{usesFamilyVaultPolicy1, usesFamilyVaultPolicy2})

	// Lane B, EDGE: EKS Pod Identity exact chain whose role carries one
	// token policy.
	addServiceAccount(usesFamilyServiceAccountB)
	addWorkloadUse(usesFamilyServiceAccountB, "b")
	addPodIdentity(usesFamilyServiceAccountB, usesFamilyRoleARNB)
	addPrincipal(usesFamilyRoleARNB)
	addPodIdentityTrust(usesFamilyRoleARNB)
	appendUsesVaultPolicyVaultRole(&factsForOdu, oduName, scopeID, generationID,
		usesFamilyVaultRole2, []string{usesFamilyServiceAccountB}, false,
		[]string{usesFamilyVaultPolicy3})

	// Lane C, NO EDGE: full surroundings but no Vault role bound to the
	// service account, so the chain cannot take the Vault hop.
	addServiceAccount(usesFamilyServiceAccountC)
	addWorkloadUse(usesFamilyServiceAccountC, "c")
	addIRSA(usesFamilyServiceAccountC, usesFamilyRoleARNC, usesFamilySubjectC)
	addPrincipal(usesFamilyRoleARNC)
	addWebIdentityTrust(usesFamilyRoleARNC, usesFamilySubjectC, "Allow", false)

	// NO EDGE: a Vault role bound to a service account nobody observed
	// never joins any chain.
	appendUsesVaultPolicyVaultRole(&factsForOdu, oduName, scopeID, generationID,
		usesFamilyVaultRole3, []string{"sha256:sa-uses-ghost"}, false, nil)

	// Lane D, NO EDGE: wildcard web-identity subject trust never matches
	// exactly, so the role's trust cannot anchor a chain.
	addServiceAccount(usesFamilyServiceAccountD)
	addWorkloadUse(usesFamilyServiceAccountD, "d")
	addIRSA(usesFamilyServiceAccountD, usesFamilyRoleARND, usesFamilySubjectD)
	addPrincipal(usesFamilyRoleARND)
	addWebIdentityTrust(usesFamilyRoleARND, usesFamilySubjectD, "Allow", true)

	// Lane E, NO EDGE: a wildcard Vault bound-service-account selector
	// refuses the exact join, so the role contributes no chain.
	addServiceAccount(usesFamilyServiceAccountE)
	addWorkloadUse(usesFamilyServiceAccountE, "e")
	addPodIdentity(usesFamilyServiceAccountE, usesFamilyRoleARNE)
	addPrincipal(usesFamilyRoleARNE)
	addPodIdentityTrust(usesFamilyRoleARNE)
	appendUsesVaultPolicyVaultRole(&factsForOdu, oduName, scopeID, generationID,
		usesFamilyVaultRole4, []string{usesFamilyServiceAccountE}, true, nil)

	// Lane F, NO EDGE: a Deny trust statement grants nothing.
	addServiceAccount(usesFamilyServiceAccountF)
	addWorkloadUse(usesFamilyServiceAccountF, "f")
	addIRSA(usesFamilyServiceAccountF, usesFamilyRoleARNF, usesFamilySubjectF)
	addPrincipal(usesFamilyRoleARNF)
	addWebIdentityTrust(usesFamilyRoleARNF, usesFamilySubjectF, "Deny", false)
	appendUsesVaultPolicyVaultRole(&factsForOdu, oduName, scopeID, generationID,
		usesFamilyVaultRole5, []string{usesFamilyServiceAccountF}, false, nil)

	return CatalogOdu{
		Odu:    Odu{Name: UsesVaultPolicyFamilyOduName, Facts: factsForOdu},
		Detail: "thirty-five facts for the direct-materialization uses_vault_policy family: two exact chains with Vault hops across both identity mechanisms (IRSA web-identity and EKS Pod Identity) whose roles carry three token policies producing three edges, and five deliberate non-producers covering the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny branches, so the expected set proves both Vault policy use and restraint",
	}
}
