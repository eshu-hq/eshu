// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// The authenticates_vault_role family Odù (#6228 slice A, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to the
// secrets/IAM graph writer through the WriteAuthenticatesVaultRoleEdges port
// of the DomainSecretsIAMGraphProjection handler. The relationship type is
// SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE, read off
// secretsIAMAuthenticatesVaultRoleEdgeUpsertCypher's MERGE, never derived
// from the port or family name.
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
	// AuthenticatesVaultRoleFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:authenticates_vault_role coverage row names
	// to resolve through it.
	AuthenticatesVaultRoleFamilyOduName = "odu:ifa-authenticates-vault-role-family"

	// authenticatesVaultRoleFamilyScopeID is the single scope every fact in
	// this Odù belongs to.
	authenticatesVaultRoleFamilyScopeID = "vault:eshu-fixture-authenticates-account"

	// authenticatesVaultRoleFamilyGenerationID is the one scope generation
	// the Odù replays.
	authenticatesVaultRoleFamilyGenerationID = "gen-ifa-authenticates-vault-role-family-1"
)

// The fixture join keys, fingerprints, and ARNs. Two service accounts carry
// exact chains through both identity mechanisms (IRSA web-identity and EKS
// Pod Identity) and produce the two edges; every other lane is a deliberate
// non-producer proving the chain resolves to nothing where it must.
const (
	authFamilyServiceAccountA = "sha256:sa-auth-app-a"
	authFamilyServiceAccountB = "sha256:sa-auth-app-b"
	authFamilyServiceAccountC = "sha256:sa-auth-app-c"
	authFamilyServiceAccountD = "sha256:sa-auth-app-d"
	authFamilyServiceAccountE = "sha256:sa-auth-app-e"
	authFamilyServiceAccountF = "sha256:sa-auth-app-f"

	authFamilyRoleARNA = "arn:aws:iam::123456789012:role/auth-app-a"
	authFamilyRoleARNB = "arn:aws:iam::123456789012:role/auth-app-b"
	authFamilyRoleARNC = "arn:aws:iam::123456789012:role/auth-app-c"
	authFamilyRoleARND = "arn:aws:iam::123456789012:role/auth-app-d"
	authFamilyRoleARNE = "arn:aws:iam::123456789012:role/auth-app-e"
	authFamilyRoleARNF = "arn:aws:iam::123456789012:role/auth-app-f"

	authFamilySubjectA = "sha256:auth-web-identity-subject-a"
	authFamilySubjectC = "sha256:auth-web-identity-subject-c"
	authFamilySubjectD = "sha256:auth-web-identity-subject-d"
	authFamilySubjectF = "sha256:auth-web-identity-subject-f"

	authFamilyVaultRole1 = "sha256:auth-vault-role-1"
	authFamilyVaultRole2 = "sha256:auth-vault-role-2"
	authFamilyVaultRole3 = "sha256:auth-vault-role-3"
	authFamilyVaultRole4 = "sha256:auth-vault-role-4"
	authFamilyVaultRole5 = "sha256:auth-vault-role-5"

	authFamilyMount = "sha256:auth-vault-mount"
)

// authenticatesVaultRoleFamilyWorkloadID is the stable workload identifier
// every workload identity-use fact carries, suffixed per lane.
func authenticatesVaultRoleFamilyWorkloadID(lane string) string {
	return "wl-auth-" + lane
}

// AuthenticatesVaultRoleFamilyOdu builds the cataloged Odù for the
// authenticates_vault_role direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanAssumeFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func AuthenticatesVaultRoleFamilyOdu() CatalogOdu {
	const oduName = AuthenticatesVaultRoleFamilyOduName
	const scopeID = authenticatesVaultRoleFamilyScopeID
	const generationID = authenticatesVaultRoleFamilyGenerationID
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
		workloadID := authenticatesVaultRoleFamilyWorkloadID(lane)
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
	addVaultRole := func(roleKey string, bound []string, wildcard bool) {
		mount := authFamilyMount
		factsForOdu = append(factsForOdu, newSecretsIAMFixtureEnvelope(
			oduName, factsidentity.VaultAuthRoleFactKind,
			scopeID, generationID, roleKey,
			mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.VaultAuthRole{
				RoleJoinKey:                         roleKey,
				MountJoinKey:                        &mount,
				BoundServiceAccountJoinKeys:         bound,
				BoundServiceAccountSelectorWildcard: &wildcard,
			}),
		))
	}

	// Lane A, EDGE: IRSA web-identity exact chain with a Vault hop.
	addServiceAccount(authFamilyServiceAccountA)
	addWorkloadUse(authFamilyServiceAccountA, "a")
	addIRSA(authFamilyServiceAccountA, authFamilyRoleARNA, authFamilySubjectA)
	addPrincipal(authFamilyRoleARNA)
	addWebIdentityTrust(authFamilyRoleARNA, authFamilySubjectA, "Allow", false)
	addVaultRole(authFamilyVaultRole1, []string{authFamilyServiceAccountA}, false)

	// Lane B, EDGE: EKS Pod Identity exact chain with a Vault hop.
	addServiceAccount(authFamilyServiceAccountB)
	addWorkloadUse(authFamilyServiceAccountB, "b")
	addPodIdentity(authFamilyServiceAccountB, authFamilyRoleARNB)
	addPrincipal(authFamilyRoleARNB)
	addPodIdentityTrust(authFamilyRoleARNB)
	addVaultRole(authFamilyVaultRole2, []string{authFamilyServiceAccountB}, false)

	// Lane C, NO EDGE: full surroundings but no Vault role bound to the
	// service account, so the chain cannot take the Vault hop.
	addServiceAccount(authFamilyServiceAccountC)
	addWorkloadUse(authFamilyServiceAccountC, "c")
	addIRSA(authFamilyServiceAccountC, authFamilyRoleARNC, authFamilySubjectC)
	addPrincipal(authFamilyRoleARNC)
	addWebIdentityTrust(authFamilyRoleARNC, authFamilySubjectC, "Allow", false)

	// NO EDGE: a Vault role bound to a service account nobody observed
	// never joins any chain.
	addVaultRole(authFamilyVaultRole3, []string{"sha256:sa-auth-ghost"}, false)

	// Lane D, NO EDGE: wildcard web-identity subject trust never matches
	// exactly, so the role's trust cannot anchor a chain.
	addServiceAccount(authFamilyServiceAccountD)
	addWorkloadUse(authFamilyServiceAccountD, "d")
	addIRSA(authFamilyServiceAccountD, authFamilyRoleARND, authFamilySubjectD)
	addPrincipal(authFamilyRoleARND)
	addWebIdentityTrust(authFamilyRoleARND, authFamilySubjectD, "Allow", true)

	// Lane E, NO EDGE: a wildcard Vault bound-service-account selector
	// refuses the exact join, so the role contributes no chain.
	addServiceAccount(authFamilyServiceAccountE)
	addWorkloadUse(authFamilyServiceAccountE, "e")
	addPodIdentity(authFamilyServiceAccountE, authFamilyRoleARNE)
	addPrincipal(authFamilyRoleARNE)
	addPodIdentityTrust(authFamilyRoleARNE)
	addVaultRole(authFamilyVaultRole4, []string{authFamilyServiceAccountE}, true)

	// Lane F, NO EDGE: a Deny trust statement grants nothing.
	addServiceAccount(authFamilyServiceAccountF)
	addWorkloadUse(authFamilyServiceAccountF, "f")
	addIRSA(authFamilyServiceAccountF, authFamilyRoleARNF, authFamilySubjectF)
	addPrincipal(authFamilyRoleARNF)
	addWebIdentityTrust(authFamilyRoleARNF, authFamilySubjectF, "Deny", false)
	addVaultRole(authFamilyVaultRole5, []string{authFamilyServiceAccountF}, false)

	return CatalogOdu{
		Odu:    Odu{Name: AuthenticatesVaultRoleFamilyOduName, Facts: factsForOdu},
		Detail: "thirty-five facts for the direct-materialization authenticates_vault_role family: two exact chains with Vault hops across both identity mechanisms (IRSA web-identity and EKS Pod Identity) producing two edges, and five deliberate non-producers covering the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny branches, so the expected set proves both Vault authentication and restraint",
	}
}
