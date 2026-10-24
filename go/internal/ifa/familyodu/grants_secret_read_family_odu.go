// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

// appendGrantsServiceAccount appends one k8s_service_account fact: the
// fixture identity every other lane fact joins to.
func appendGrantsServiceAccount(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.KubernetesServiceAccountFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesServiceAccount{
			ServiceAccountJoinKey: joinKey,
		}),
	))
}

// appendGrantsWorkloadUse appends one k8s_workload_identity_use fact
// binding the lane's service account to its workload.
func appendGrantsWorkloadUse(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, lane string) {
	workloadID := grantsSecretReadFamilyWorkloadID(lane)
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

// appendGrantsIRSA appends one eks_irsa_annotation fact: the web-identity
// binding the reducer's exactWebIdentityTrust reads.
func appendGrantsIRSA(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN, subject string) {
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

// appendGrantsPodIdentity appends one eks_pod_identity_association fact.
func appendGrantsPodIdentity(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.EKSPodIdentityAssociationFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSPodIdentityAssociation{
			ServiceAccountJoinKey: joinKey,
			RoleARN:               roleARN,
		}),
	))
}

// appendGrantsPrincipal appends one aws_iam_principal fact for a lane's
// IAM role.
func appendGrantsPrincipal(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
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

// appendGrantsEndpointObservations appends the endpoint observations the
// live projection's cross-scope readiness gate requires: pod-template facts
// for every exact-chain lane's workload and aws_resource facts for every
// exact-chain lane's IAM role. See appendUsesEndpointObservations for why
// the family carries its own observations.
//
// Every lane below that resolves an exact identity chain (a, b, 7, and 8)
// produces uses_service_account and assumes_iam_role rows, so the
// readiness gate looks their workload and cloud endpoints up uid-exactly:
// all four lanes need observations, not just the two that yield grants
// edges. The chain-negative lanes (c through f) resolve no identity chain
// and reference no endpoint, so they carry none.
func appendGrantsEndpointObservations(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendVaultWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-grants", grantsSecretReadFamilyWorkloadID("a"), "a", "grants-app-a")
	appendVaultWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-grants", grantsSecretReadFamilyWorkloadID("b"), "b", "grants-app-b")
	appendVaultWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-grants", grantsSecretReadFamilyWorkloadID("7"), "7", "grants-app-7")
	appendVaultWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-grants", grantsSecretReadFamilyWorkloadID("8"), "8", "grants-app-8")
	appendVaultIAMRoleResource(factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNA, "grants-app-a")
	appendVaultIAMRoleResource(factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNB, "grants-app-b")
	appendVaultIAMRoleResource(factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNG, "grants-app-7")
	appendVaultIAMRoleResource(factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNH, "grants-app-8")
}

// The grants_secret_read family Odù (#6228 remainder, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to the
// secrets/IAM graph writer through the WriteGrantsSecretReadEdges port of
// the DomainSecretsIAMGraphProjection handler. The relationship type is
// SECRETS_IAM_GRANTS_SECRET_READ, read off
// secretsIAMGrantsSecretReadEdgeUpsertCypher's MERGE, never derived from
// the port or family name.
//
// Facts are built as typed secretsiamv1 / iamv1 values and encoded through
// the shared fixture seam, never as hand-built maps (Contract System v1).
// The aws_iam_trust_policy facts use the secretsiamv1.AWSIAMTrustPolicy
// struct with the full source context the schema requires; the
// vault_acl_policy facts use secretsiamv1.VaultACLPolicy with one typed
// rule entry per governed path, and the vault_kv_metadata facts use
// secretsiamv1.VaultKVMetadata with the mount key and path fingerprint the
// reducer joins on.
//
// Every fact shares one scope generation: the trust-chain handler loads one
// scope generation's source facts, so a fixture spanning scopes would not
// mirror any real intent. The scope is deliberately NOT any scope a sibling
// cassette owns: scopes carry one ACTIVE generation, so driving a second
// generation into an occupied scope supersedes the first family's
// generation (the iam_can_assume lesson).

const (
	// GrantsSecretReadFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:grants_secret_read coverage row names to resolve
	// through it.
	GrantsSecretReadFamilyOduName = "odu:ifa-grants-secret-read-family"

	// grantsSecretReadFamilyScopeID is the single scope every fact in this
	// Odù belongs to.
	grantsSecretReadFamilyScopeID = "vault:eshu-fixture-grants-account"

	// grantsSecretReadFamilyGenerationID is the one scope generation the
	// Odù replays.
	grantsSecretReadFamilyGenerationID = "gen-ifa-grants-secret-read-family-1"
)

// The fixture join keys, fingerprints, and ARNs. Two exact lanes resolve
// grants edges across both identity mechanisms (IRSA web-identity and EKS
// Pod Identity): the IRSA lane's role carries two token policies with one
// read rule each, fanning out to two edges, and the Pod Identity lane's
// role carries one policy with one read rule. Two further exact lanes prove
// grants-specific restraint: a list-only rule grants nothing, and a read
// rule with no matching KV metadata resolves to nothing. Four chain
// lanes prove the chain resolves to nothing where it must.
const (
	grantsFamilyServiceAccountA = "sha256:sa-grants-app-a"
	grantsFamilyServiceAccountB = "sha256:sa-grants-app-b"
	grantsFamilyServiceAccountC = "sha256:sa-grants-app-c"
	grantsFamilyServiceAccountD = "sha256:sa-grants-app-d"
	grantsFamilyServiceAccountE = "sha256:sa-grants-app-e"
	grantsFamilyServiceAccountF = "sha256:sa-grants-app-f"
	grantsFamilyServiceAccount7 = "sha256:sa-grants-app-7"
	grantsFamilyServiceAccount8 = "sha256:sa-grants-app-8"

	grantsFamilyRoleARNA = "arn:aws:iam::123456789012:role/grants-app-a"
	grantsFamilyRoleARNB = "arn:aws:iam::123456789012:role/grants-app-b"
	grantsFamilyRoleARNC = "arn:aws:iam::123456789012:role/grants-app-c"
	grantsFamilyRoleARND = "arn:aws:iam::123456789012:role/grants-app-d"
	grantsFamilyRoleARNE = "arn:aws:iam::123456789012:role/grants-app-e"
	grantsFamilyRoleARNF = "arn:aws:iam::123456789012:role/grants-app-f"
	grantsFamilyRoleARNG = "arn:aws:iam::123456789012:role/grants-app-7"
	grantsFamilyRoleARNH = "arn:aws:iam::123456789012:role/grants-app-8"

	grantsFamilySubjectA = "sha256:grants-web-identity-subject-a"
	grantsFamilySubjectC = "sha256:grants-web-identity-subject-c"
	grantsFamilySubjectD = "sha256:grants-web-identity-subject-d"
	grantsFamilySubjectF = "sha256:grants-web-identity-subject-f"
	grantsFamilySubject7 = "sha256:grants-web-identity-subject-7"
	grantsFamilySubject8 = "sha256:grants-web-identity-subject-8"

	grantsFamilyVaultRole1 = "sha256:grants-vault-role-1"
	grantsFamilyVaultRole2 = "sha256:grants-vault-role-2"
	grantsFamilyVaultRole3 = "sha256:grants-vault-role-3"
	grantsFamilyVaultRole4 = "sha256:grants-vault-role-4"
	grantsFamilyVaultRole5 = "sha256:grants-vault-role-5"
	grantsFamilyVaultRole6 = "sha256:grants-vault-role-6"
	grantsFamilyVaultRole7 = "sha256:grants-vault-role-7"

	grantsFamilyVaultPolicy1 = "sha256:grants-vault-policy-1"
	grantsFamilyVaultPolicy2 = "sha256:grants-vault-policy-2"
	grantsFamilyVaultPolicy3 = "sha256:grants-vault-policy-3"
	grantsFamilyVaultPolicy4 = "sha256:grants-vault-policy-4"
	grantsFamilyVaultPolicy5 = "sha256:grants-vault-policy-5"

	grantsFamilyMount = "sha256:grants-vault-mount"

	grantsFamilyKVPath1 = "sha256:grants-kv-path-1"
	grantsFamilyKVPath2 = "sha256:grants-kv-path-2"
	grantsFamilyKVPath3 = "sha256:grants-kv-path-3"
	grantsFamilyKVPath4 = "sha256:grants-kv-path-4"
	grantsFamilyKVPath9 = "sha256:grants-kv-path-9"
)

// grantsSecretReadFamilyWorkloadID is the stable workload identifier every
// workload identity-use fact carries, suffixed per lane.
func grantsSecretReadFamilyWorkloadID(lane string) string {
	return "wl-grants-" + lane
}

// appendGrantsVaultRole appends the Vault auth-role fact. policies lists
// the token policy join keys the role's issued tokens carry: the grants
// edges fan out one per read rule per key, so the exact lanes carry
// policies and every chain-negative role carries none.
func appendGrantsVaultRole(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleKey string, bound []string, wildcard bool, policies []string) {
	mount := grantsFamilyMount
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

// appendGrantsWebIdentityTrust appends the aws_iam_trust_policy fact
// carrying the web-identity subject the reducer's exactWebIdentityTrust
// reads, with the full source context the schema requires.
func appendGrantsWebIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN, subject, effect string, wildcard bool) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.AWSIAMTrustPolicyFactKind,
		scopeID, generationID, roleARN+":"+effect,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.AWSIAMTrustPolicy{
			AccountID:                      "123456789012",
			Region:                         "us-east-1",
			Provider:                       "aws_iam",
			CollectorInstanceID:            "collector-secrets-iam-fixture",
			RedactionPolicyVersion:         "secrets_iam_posture_v1",
			RoleARN:                        roleARN,
			PolicySource:                   "trust",
			Effect:                         effect,
			Actions:                        []string{"sts:assumerolewithwebidentity"},
			WebIdentitySubjectFingerprints: []string{subject},
			WebIdentitySubjectWildcard:     &wildcard,
		}),
	))
}

// appendGrantsPodIdentityTrust appends the Pod Identity trust fact with the
// full source context the schema requires.
func appendGrantsPodIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.AWSIAMTrustPolicyFactKind,
		scopeID, generationID, roleARN+":pod-identity",
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.AWSIAMTrustPolicy{
			AccountID:              "123456789012",
			Region:                 "us-east-1",
			Provider:               "aws_iam",
			CollectorInstanceID:    "collector-secrets-iam-fixture",
			RedactionPolicyVersion: "secrets_iam_posture_v1",
			RoleARN:                roleARN,
			PolicySource:           "trust",
			Effect:                 "Allow",
			Actions:                []string{"sts:AssumeRole"},
			AssumePrincipals:       []string{"pods.eks.amazonaws.com"},
		}),
	))
}

// appendGrantsACLPolicy appends one vault_acl_policy fact: the redacted
// rule summary for one token policy. Each rule pairs a path fingerprint
// with the capabilities the policy grants on it; only a read capability
// resolves a grants edge.
func appendGrantsACLPolicy(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, policyKey string, rules []secretsiamv1.VaultACLPolicyRule) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.VaultACLPolicyFactKind,
		scopeID, generationID, policyKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.VaultACLPolicy{
			PolicyJoinKey: policyKey,
			Rules:         rules,
		}),
	))
}

// grantsReadRule builds one vault_acl_policy rule entry granting the given
// capabilities on a path fingerprint. The depth mirrors what the collector
// emits but the reducer never reads it.
func grantsReadRule(pathFingerprint string, capabilities ...string) secretsiamv1.VaultACLPolicyRule {
	depth := 3
	return secretsiamv1.VaultACLPolicyRule{
		PathFingerprint: &pathFingerprint,
		PathDepth:       &depth,
		Capabilities:    capabilities,
	}
}

// appendGrantsKVMetadata appends one vault_kv_metadata fact: the
// redacted path identity one ACL rule joins to. A read rule with no
// matching metadata resolves to nothing.
func appendGrantsKVMetadata(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, kvFingerprint string) {
	depth := 3
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.VaultKVMetadataFactKind,
		scopeID, generationID, kvFingerprint,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.VaultKVMetadata{
			MountJoinKey:      grantsFamilyMount,
			KVPathFingerprint: kvFingerprint,
			PathDepth:         &depth,
		}),
	))
}

// GrantsSecretReadFamilyOdu builds the cataloged Odù for the
// grants_secret_read direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanAssumeFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func GrantsSecretReadFamilyOdu() CatalogOdu {
	const oduName = GrantsSecretReadFamilyOduName
	const scopeID = grantsSecretReadFamilyScopeID
	const generationID = grantsSecretReadFamilyGenerationID
	factsForOdu := make([]facts.Envelope, 0, 64)

	addServiceAccount := func(joinKey string) {
		appendGrantsServiceAccount(&factsForOdu, oduName, scopeID, generationID, joinKey)
	}
	addWorkloadUse := func(joinKey, lane string) {
		appendGrantsWorkloadUse(&factsForOdu, oduName, scopeID, generationID, joinKey, lane)
	}
	addIRSA := func(joinKey, roleARN, subject string) {
		appendGrantsIRSA(&factsForOdu, oduName, scopeID, generationID, joinKey, roleARN, subject)
	}
	addPodIdentity := func(joinKey, roleARN string) {
		appendGrantsPodIdentity(&factsForOdu, oduName, scopeID, generationID, joinKey, roleARN)
	}
	addPrincipal := func(roleARN string) {
		appendGrantsPrincipal(&factsForOdu, oduName, scopeID, generationID, roleARN)
	}
	// Lane A, EDGES: IRSA web-identity exact chain whose role carries two
	// token policies with one read rule each, fanning out to two grants
	// edges.
	addServiceAccount(grantsFamilyServiceAccountA)
	addWorkloadUse(grantsFamilyServiceAccountA, "a")
	addIRSA(grantsFamilyServiceAccountA, grantsFamilyRoleARNA, grantsFamilySubjectA)
	addPrincipal(grantsFamilyRoleARNA)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNA, grantsFamilySubjectA, "Allow", false)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole1, []string{grantsFamilyServiceAccountA}, false,
		[]string{grantsFamilyVaultPolicy1, grantsFamilyVaultPolicy2})

	// Lane B, EDGE: EKS Pod Identity exact chain whose role carries one
	// token policy with one read rule.
	addServiceAccount(grantsFamilyServiceAccountB)
	addWorkloadUse(grantsFamilyServiceAccountB, "b")
	addPodIdentity(grantsFamilyServiceAccountB, grantsFamilyRoleARNB)
	addPrincipal(grantsFamilyRoleARNB)
	appendGrantsPodIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNB)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole2, []string{grantsFamilyServiceAccountB}, false,
		[]string{grantsFamilyVaultPolicy3})

	// Lane C, NO EDGE: full surroundings but no Vault role bound to the
	// service account, so the chain cannot take the Vault hop.
	addServiceAccount(grantsFamilyServiceAccountC)
	addWorkloadUse(grantsFamilyServiceAccountC, "c")
	addIRSA(grantsFamilyServiceAccountC, grantsFamilyRoleARNC, grantsFamilySubjectC)
	addPrincipal(grantsFamilyRoleARNC)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNC, grantsFamilySubjectC, "Allow", false)

	// NO EDGE: a Vault role bound to a service account nobody observed
	// never joins any chain.
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole7, []string{"sha256:sa-grants-ghost"}, false, nil)

	// Lane D, NO EDGE: wildcard web-identity subject trust never matches
	// exactly, so the role's trust cannot anchor a chain.
	addServiceAccount(grantsFamilyServiceAccountD)
	addWorkloadUse(grantsFamilyServiceAccountD, "d")
	addIRSA(grantsFamilyServiceAccountD, grantsFamilyRoleARND, grantsFamilySubjectD)
	addPrincipal(grantsFamilyRoleARND)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARND, grantsFamilySubjectD, "Allow", true)

	// Lane E, NO EDGE: a wildcard Vault bound-service-account selector
	// refuses the exact join, so the role contributes no chain.
	addServiceAccount(grantsFamilyServiceAccountE)
	addWorkloadUse(grantsFamilyServiceAccountE, "e")
	addPodIdentity(grantsFamilyServiceAccountE, grantsFamilyRoleARNE)
	addPrincipal(grantsFamilyRoleARNE)
	appendGrantsPodIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNE)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole5, []string{grantsFamilyServiceAccountE}, true, nil)

	// Lane F, NO EDGE: a Deny trust statement grants nothing.
	addServiceAccount(grantsFamilyServiceAccountF)
	addWorkloadUse(grantsFamilyServiceAccountF, "f")
	addIRSA(grantsFamilyServiceAccountF, grantsFamilyRoleARNF, grantsFamilySubjectF)
	addPrincipal(grantsFamilyRoleARNF)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNF, grantsFamilySubjectF, "Deny", false)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole6, []string{grantsFamilyServiceAccountF}, false, nil)

	// Lane 7, NO EDGE: exact chain whose policy's only rule grants list,
	// never read. The KV metadata exists on purpose: the miss is the
	// capability, not a missing path.
	addServiceAccount(grantsFamilyServiceAccount7)
	addWorkloadUse(grantsFamilyServiceAccount7, "7")
	addIRSA(grantsFamilyServiceAccount7, grantsFamilyRoleARNG, grantsFamilySubject7)
	addPrincipal(grantsFamilyRoleARNG)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNG, grantsFamilySubject7, "Allow", false)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole3, []string{grantsFamilyServiceAccount7}, false,
		[]string{grantsFamilyVaultPolicy4})

	// Lane 8, NO EDGE: exact chain whose policy's read rule names a path
	// with no KV metadata. The chain resolves; the path does not.
	addServiceAccount(grantsFamilyServiceAccount8)
	addWorkloadUse(grantsFamilyServiceAccount8, "8")
	addIRSA(grantsFamilyServiceAccount8, grantsFamilyRoleARNH, grantsFamilySubject8)
	addPrincipal(grantsFamilyRoleARNH)
	appendGrantsWebIdentityTrust(&factsForOdu, oduName, scopeID, generationID, grantsFamilyRoleARNH, grantsFamilySubject8, "Allow", false)
	appendGrantsVaultRole(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultRole4, []string{grantsFamilyServiceAccount8}, false,
		[]string{grantsFamilyVaultPolicy5})

	// The redacted rule summaries the exact lanes' token policies carry.
	// Lane A's role fans out across two policies with one read rule each;
	// lane B's role carries one policy with one read rule.
	appendGrantsACLPolicy(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultPolicy1, []secretsiamv1.VaultACLPolicyRule{
			grantsReadRule(grantsFamilyKVPath1, "read"),
		})
	appendGrantsACLPolicy(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultPolicy2, []secretsiamv1.VaultACLPolicyRule{
			grantsReadRule(grantsFamilyKVPath2, "read"),
		})
	appendGrantsACLPolicy(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultPolicy3, []secretsiamv1.VaultACLPolicyRule{
			grantsReadRule(grantsFamilyKVPath3, "read"),
		})
	// Lane 7's policy grants only list on an existing path.
	appendGrantsACLPolicy(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultPolicy4, []secretsiamv1.VaultACLPolicyRule{
			grantsReadRule(grantsFamilyKVPath4, "list"),
		})
	// Lane 8's policy grants read on a path with no KV metadata.
	appendGrantsACLPolicy(&factsForOdu, oduName, scopeID, generationID,
		grantsFamilyVaultPolicy5, []secretsiamv1.VaultACLPolicyRule{
			grantsReadRule(grantsFamilyKVPath9, "read"),
		})

	// The KV metadata every read rule but lane 8's joins to.
	appendGrantsKVMetadata(&factsForOdu, oduName, scopeID, generationID, grantsFamilyKVPath1)
	appendGrantsKVMetadata(&factsForOdu, oduName, scopeID, generationID, grantsFamilyKVPath2)
	appendGrantsKVMetadata(&factsForOdu, oduName, scopeID, generationID, grantsFamilyKVPath3)
	appendGrantsKVMetadata(&factsForOdu, oduName, scopeID, generationID, grantsFamilyKVPath4)

	appendGrantsEndpointObservations(&factsForOdu, oduName, scopeID, generationID)

	return CatalogOdu{
		Odu:    Odu{Name: GrantsSecretReadFamilyOduName, Facts: factsForOdu},
		Detail: "sixty-four facts for the direct-materialization grants_secret_read family: two exact chains with Vault hops across both identity mechanisms (IRSA web-identity and EKS Pod Identity) whose policies carry three read rules resolving to three edges, four deliberate chain non-producers covering the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny branches, two grants-specific non-producers covering the list-only capability and the missing-KV-metadata branches, and eight endpoint observations (pod templates and aws_resource facts for every exact chain's workload and IAM role) so the live projection's endpoint-readiness gate finds the cross-scope endpoints no other scope commits, so the expected set proves both secret-read grants and restraint",
	}
}
