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
// SecretsIAMAuthenticatesVaultRoleEdgeUpsertCypher's MERGE, never derived
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

// appendAuthServiceAccount appends the Kubernetes service-account fact.
func appendAuthServiceAccount(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.KubernetesServiceAccountFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesServiceAccount{
			ServiceAccountJoinKey: joinKey,
		}),
	))
}

// appendAuthWorkloadUse appends the workload identity-use fact for one lane.
func appendAuthWorkloadUse(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, lane string) {
	workloadID := authenticatesVaultRoleFamilyWorkloadID(lane)
	workloadKind := "deployments"
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.KubernetesWorkloadIdentityUseFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesWorkloadIdentityUse{
			ServiceAccountJoinKey: joinKey,
			WorkloadObjectID:      &workloadID,
			WorkloadKind:          &workloadKind,
		}),
	))
}

// appendAuthIRSA appends the EKS IRSA annotation fact.
func appendAuthIRSA(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN, subject string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.EKSIRSAAnnotationFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSIRSAAnnotation{
			ServiceAccountJoinKey:         joinKey,
			RoleARN:                       roleARN,
			WebIdentitySubjectFingerprint: &subject,
		}),
	))
}

// appendAuthPodIdentity appends the EKS Pod Identity association fact.
func appendAuthPodIdentity(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.EKSPodIdentityAssociationFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSPodIdentityAssociation{
			ServiceAccountJoinKey: joinKey,
			RoleARN:               roleARN,
		}),
	))
}

// appendAuthPrincipal appends the AWS IAM principal fact.
func appendAuthPrincipal(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
	provider := "aws_iam"
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
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

// appendAuthWebIdentityTrust appends a raw-map aws_iam_trust_policy fact: the
// kind has no typed struct yet, so the map mirrors exactly what the
// reducer's exactWebIdentityTrust reads.
func appendAuthWebIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN, subject, effect string, wildcard bool) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
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

// appendAuthPodIdentityTrust appends the raw-map Pod Identity trust fact.
func appendAuthPodIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
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

// appendAuthVaultRole appends the Vault auth-role fact bound to the given
// service accounts.
func appendAuthVaultRole(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleKey string, bound []string, wildcard bool) {
	mount := authFamilyMount
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
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

// appendAuthenticatesLaneA appends lane A, EDGE: IRSA web-identity exact
// chain with a Vault hop.
func appendAuthenticatesLaneA(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountA)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountA, "a")
	appendAuthIRSA(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountA, authFamilyRoleARNA, authFamilySubjectA)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNA)
	appendAuthWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNA, authFamilySubjectA, "Allow", false)
	appendAuthVaultRole(factsForOdu, oduName, scopeID, generationID, authFamilyVaultRole1, []string{authFamilyServiceAccountA}, false)
}

// appendAuthenticatesLaneB appends lane B, EDGE: EKS Pod Identity exact
// chain with a Vault hop.
func appendAuthenticatesLaneB(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountB)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountB, "b")
	appendAuthPodIdentity(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountB, authFamilyRoleARNB)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNB)
	appendAuthPodIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNB)
	appendAuthVaultRole(factsForOdu, oduName, scopeID, generationID, authFamilyVaultRole2, []string{authFamilyServiceAccountB}, false)
}

// appendAuthenticatesLaneC appends lane C, NO EDGE: full surroundings but no
// Vault role bound to the service account, so the chain cannot take the
// Vault hop. The ghost-bound role never joins any chain either.
func appendAuthenticatesLaneC(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountC)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountC, "c")
	appendAuthIRSA(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountC, authFamilyRoleARNC, authFamilySubjectC)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNC)
	appendAuthWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNC, authFamilySubjectC, "Allow", false)
	appendAuthVaultRole(factsForOdu, oduName, scopeID, generationID, authFamilyVaultRole3, []string{"sha256:sa-auth-ghost"}, false)
}

// appendAuthenticatesLaneD appends lane D, NO EDGE: wildcard web-identity
// subject trust never matches exactly, so the role's trust cannot anchor a
// chain.
func appendAuthenticatesLaneD(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountD)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountD, "d")
	appendAuthIRSA(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountD, authFamilyRoleARND, authFamilySubjectD)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARND)
	appendAuthWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARND, authFamilySubjectD, "Allow", true)
}

// appendAuthenticatesLaneE appends lane E, NO EDGE: a wildcard Vault
// bound-service-account selector refuses the exact join, so the role
// contributes no chain.
func appendAuthenticatesLaneE(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountE)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountE, "e")
	appendAuthPodIdentity(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountE, authFamilyRoleARNE)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNE)
	appendAuthPodIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNE)
	appendAuthVaultRole(factsForOdu, oduName, scopeID, generationID, authFamilyVaultRole4, []string{authFamilyServiceAccountE}, true)
}

// appendAuthenticatesLaneF appends lane F, NO EDGE: a Deny trust statement
// grants nothing.
func appendAuthenticatesLaneF(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAuthServiceAccount(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountF)
	appendAuthWorkloadUse(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountF, "f")
	appendAuthIRSA(factsForOdu, oduName, scopeID, generationID, authFamilyServiceAccountF, authFamilyRoleARNF, authFamilySubjectF)
	appendAuthPrincipal(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNF)
	appendAuthWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, authFamilyRoleARNF, authFamilySubjectF, "Deny", false)
	appendAuthVaultRole(factsForOdu, oduName, scopeID, generationID, authFamilyVaultRole5, []string{authFamilyServiceAccountF}, false)
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

	appendAuthenticatesLaneA(&factsForOdu, oduName, scopeID, generationID)
	appendAuthenticatesLaneB(&factsForOdu, oduName, scopeID, generationID)
	appendAuthenticatesLaneC(&factsForOdu, oduName, scopeID, generationID)
	appendAuthenticatesLaneD(&factsForOdu, oduName, scopeID, generationID)
	appendAuthenticatesLaneE(&factsForOdu, oduName, scopeID, generationID)
	appendAuthenticatesLaneF(&factsForOdu, oduName, scopeID, generationID)

	return CatalogOdu{
		Odu:    Odu{Name: AuthenticatesVaultRoleFamilyOduName, Facts: factsForOdu},
		Detail: "thirty-five facts for the direct-materialization authenticates_vault_role family: two exact chains with Vault hops across both identity mechanisms (IRSA web-identity and EKS Pod Identity) producing two edges, and five deliberate non-producers covering the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny branches, so the expected set proves both Vault authentication and restraint",
	}
}
