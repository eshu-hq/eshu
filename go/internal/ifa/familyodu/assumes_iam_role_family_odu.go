// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"
	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu-sdk/factschema/iam/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu-sdk/factschema/kuberneteslive/v1"
	secretsiamv1 "github.com/eshu-hq/eshu-sdk/factschema/secretsiam/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
)

// The assumes_iam_role family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to the
// secrets/IAM graph writer through the WriteAssumesIAMRoleEdges port
// of the DomainSecretsIAMGraphProjection handler. The relationship type is
// SECRETS_IAM_ASSUMES_IAM_ROLE, read off
// SecretsIAMAssumesIAMRoleEdgeUpsertCypher's MERGE, never derived
// from the port or family name.
//
// Facts are built as typed secretsiamv1 / iamv1 values and encoded through
// the shared fixture seam, never as hand-built maps (Contract System v1).
// The aws_iam_trust_policy facts use the secretsiamv1.AWSIAMTrustPolicy
// struct with the full source context the schema requires.
//
// Every fact shares one scope generation: the trust-chain handler loads one
// scope generation's source facts, so a fixture spanning scopes would not
// mirror any real intent. The scope is deliberately NOT any scope a sibling
// cassette owns: scopes carry one ACTIVE generation, so driving a second
// generation into an occupied scope supersedes the first family's
// generation (the iam_can_assume lesson).

const (
	// AssumesIAMRoleFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:assumes_iam_role coverage row names
	// to resolve through it.
	AssumesIAMRoleFamilyOduName = "odu:ifa-assumes-iam-role-family"

	// assumesIAMRoleFamilyScopeID is the single scope every fact in
	// this Odù belongs to.
	assumesIAMRoleFamilyScopeID = "vault:eshu-fixture-assumes-account"

	// assumesIAMRoleFamilyGenerationID is the one scope generation
	// the Odù replays.
	assumesIAMRoleFamilyGenerationID = "gen-ifa-assumes-iam-role-family-1"
)

// The fixture join keys, fingerprints, and ARNs. Two service accounts carry
// exact chains through both identity mechanisms (IRSA web-identity and EKS
// Pod Identity) and produce the two edges; every other lane is a deliberate
// non-producer proving the chain resolves to nothing where it must. Lane A
// binds TWO Vault roles so its two exact chains prove duplicate delivery
// converges on one edge.
const (
	assumesFamilyServiceAccountA = "sha256:sa-assumes-app-a"
	assumesFamilyServiceAccountB = "sha256:sa-assumes-app-b"
	assumesFamilyServiceAccountC = "sha256:sa-assumes-app-c"
	assumesFamilyServiceAccountD = "sha256:sa-assumes-app-d"
	assumesFamilyServiceAccountE = "sha256:sa-assumes-app-e"
	assumesFamilyServiceAccountF = "sha256:sa-assumes-app-f"

	assumesFamilyRoleARNA = "arn:aws:iam::123456789012:role/assumes-app-a"
	assumesFamilyRoleARNB = "arn:aws:iam::123456789012:role/assumes-app-b"
	assumesFamilyRoleARNC = "arn:aws:iam::123456789012:role/assumes-app-c"
	assumesFamilyRoleARND = "arn:aws:iam::123456789012:role/assumes-app-d"
	assumesFamilyRoleARNE = "arn:aws:iam::123456789012:role/assumes-app-e"
	assumesFamilyRoleARNF = "arn:aws:iam::123456789012:role/assumes-app-f"

	assumesFamilySubjectA = "sha256:assumes-web-identity-subject-a"
	assumesFamilySubjectB = "sha256:assumes-web-identity-subject-b"
	assumesFamilySubjectD = "sha256:assumes-web-identity-subject-d"
	assumesFamilySubjectE = "sha256:assumes-web-identity-subject-e"
	assumesFamilySubjectF = "sha256:assumes-web-identity-subject-f"

	assumesFamilyUnmatchedSubject = "sha256:assumes-unmatched-subject"

	assumesFamilyVaultRole1 = "sha256:assumes-vault-role-1"
	assumesFamilyVaultRole2 = "sha256:assumes-vault-role-2"
	assumesFamilyVaultRole3 = "sha256:assumes-vault-role-3"
	assumesFamilyVaultRole4 = "sha256:assumes-vault-role-4"
	assumesFamilyVaultRole5 = "sha256:assumes-vault-role-5"
	assumesFamilyVaultRole6 = "sha256:assumes-vault-role-6"
	assumesFamilyVaultRole7 = "sha256:assumes-vault-role-7"

	assumesFamilyMount = "sha256:assumes-vault-mount"
)

// assumesIAMRoleFamilyWorkloadID is the stable workload identifier
// every workload identity-use fact carries, suffixed per lane.
func assumesIAMRoleFamilyWorkloadID(lane string) string {
	return "wl-assumes-" + lane
}

// appendAssumesServiceAccount appends the Kubernetes service-account fact.
func appendAssumesServiceAccount(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.KubernetesServiceAccountFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.KubernetesServiceAccount{
			ServiceAccountJoinKey: joinKey,
		}),
	))
}

// appendAssumesWorkloadUse appends the workload identity-use fact for one lane.
func appendAssumesWorkloadUse(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, lane string) {
	workloadID := assumesIAMRoleFamilyWorkloadID(lane)
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

// appendAssumesIRSA appends the EKS IRSA annotation fact.
func appendAssumesIRSA(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN, subject string) {
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

// appendAssumesIRSASubjectless appends the EKS IRSA annotation fact carrying
// no web-identity subject fingerprint: the role reference (the fingerprint
// the collector observed) is the whole of the IAM evidence, so no trust
// statement can match it exactly.
func appendAssumesIRSASubjectless(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.EKSIRSAAnnotationFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSIRSAAnnotation{
			ServiceAccountJoinKey: joinKey,
			RoleARN:               roleARN,
		}),
	))
}

// appendAssumesPodIdentity appends the EKS Pod Identity association fact.
func appendAssumesPodIdentity(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, joinKey, roleARN string) {
	*factsForOdu = append(*factsForOdu, newSecretsIAMFixtureEnvelope(
		oduName, factsidentity.EKSPodIdentityAssociationFactKind,
		scopeID, generationID, joinKey,
		mustEncodeSecretsIAMFixturePayload(oduName, secretsiamv1.EKSPodIdentityAssociation{
			ServiceAccountJoinKey: joinKey,
			RoleARN:               roleARN,
		}),
	))
}

// appendAssumesPrincipal appends the AWS IAM principal fact.
func appendAssumesPrincipal(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
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

// appendAssumesWebIdentityTrust appends the aws_iam_trust_policy fact carrying
// the web-identity subjects the reducer's exactWebIdentityTrust reads, with
// the full source context the schema requires.
func appendAssumesWebIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN, effect string, wildcard bool, subjects []string) {
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
			WebIdentitySubjectFingerprints: subjects,
			WebIdentitySubjectWildcard:     &wildcard,
		}),
	))
}

// appendAssumesPodIdentityTrust appends the Pod Identity trust fact with the
// full source context the schema requires.
func appendAssumesPodIdentityTrust(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN string) {
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

// appendAssumesVaultRole appends the Vault auth-role fact bound to the given
// service accounts. The role carries no token policies on purpose: an exact
// chain needs no policy to assume, and the uses_vault_policy Odù owns the
// policy-fan-out proof. Keeping policies out also keeps this scope from
// spilling USES_VAULT_POLICY and GRANTS_SECRET_READ edges into the shared
// determinism cell's sibling asserts.
func appendAssumesVaultRole(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleKey string, bound []string) {
	mount := assumesFamilyMount
	wildcard := false
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

// appendAssumesLaneA appends lane A, EDGE: IRSA web-identity exact chain.
// The service account binds TWO Vault roles, so the chain resolves twice
// and the projection must converge both rows on one ASSUMES_IAM_ROLE edge:
// the duplicate-delivery proof.
func appendAssumesLaneA(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountA)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountA, "a")
	appendAssumesIRSA(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountA, assumesFamilyRoleARNA, assumesFamilySubjectA)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNA)
	appendAssumesWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNA, "Allow", false, []string{assumesFamilySubjectA})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole1, []string{assumesFamilyServiceAccountA})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole2, []string{assumesFamilyServiceAccountA})
}

// appendAssumesLaneB appends lane B, EDGE: EKS Pod Identity exact chain.
func appendAssumesLaneB(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountB)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountB, "b")
	appendAssumesPodIdentity(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountB, assumesFamilyRoleARNB)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNB)
	appendAssumesPodIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNB)
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole3, []string{assumesFamilyServiceAccountB})
}

// appendAssumesLaneC appends lane C, NO EDGE: fingerprint-only. The IRSA
// annotation carries the role reference but no web-identity subject, so no
// trust statement can match it exactly even though the role's own Allow
// trust is present.
func appendAssumesLaneC(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountC)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountC, "c")
	appendAssumesIRSASubjectless(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountC, assumesFamilyRoleARNC)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNC)
	appendAssumesWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNC, "Allow", false, []string{assumesFamilySubjectB})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole4, []string{assumesFamilyServiceAccountC})
}

// appendAssumesLaneD appends lane D, NO EDGE: partial chain. The annotation
// subject matches nothing the role's trust allows, so the trust resolves to
// a partial gap rather than an exact match.
func appendAssumesLaneD(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountD)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountD, "d")
	appendAssumesIRSA(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountD, assumesFamilyRoleARND, assumesFamilySubjectD)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARND)
	appendAssumesWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARND, "Allow", false, []string{assumesFamilyUnmatchedSubject})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole5, []string{assumesFamilyServiceAccountD})
}

// appendAssumesLaneE appends lane E, NO EDGE: a Deny trust statement
// grants nothing.
func appendAssumesLaneE(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountE)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountE, "e")
	appendAssumesIRSA(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountE, assumesFamilyRoleARNE, assumesFamilySubjectE)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNE)
	appendAssumesWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNE, "Deny", false, []string{assumesFamilySubjectE})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole6, []string{assumesFamilyServiceAccountE})
}

// appendAssumesLaneF appends lane F, NO EDGE: wildcard web-identity
// subject trust never matches exactly, so the role's trust cannot anchor a
// chain.
func appendAssumesLaneF(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesServiceAccount(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountF)
	appendAssumesWorkloadUse(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountF, "f")
	appendAssumesIRSA(factsForOdu, oduName, scopeID, generationID, assumesFamilyServiceAccountF, assumesFamilyRoleARNF, assumesFamilySubjectF)
	appendAssumesPrincipal(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNF)
	appendAssumesWebIdentityTrust(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNF, "Allow", true, []string{assumesFamilySubjectF})
	appendAssumesVaultRole(factsForOdu, oduName, scopeID, generationID, assumesFamilyVaultRole7, []string{assumesFamilyServiceAccountF})
}

// appendAssumesEndpointObservations appends the endpoint observations the live
// projection's cross-scope readiness gate requires: pod-template facts for
// the exact-chain lanes' workloads (committed as KubernetesWorkload nodes by
// the workload materialization) and aws_resource facts for the exact-chain
// lanes' IAM roles (committed as CloudResource nodes by the AWS resource
// materialization). Both endpoints are fixture-local -- no other scope in
// any gate corpus commits them -- so the family carries its own
// observations, the same self-contained posture the iam_can_assume family
// uses for its bucket resource. Only exact-chain lanes need them: no other
// lane emits a USES_SERVICE_ACCOUNT or ASSUMES_IAM_ROLE edge, so no other
// lane's endpoints are ever looked up.
func appendAssumesEndpointObservations(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID string) {
	appendAssumesWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-assumes", assumesIAMRoleFamilyWorkloadID("a"), "a", "assumes-app-a")
	appendAssumesWorkloadPodTemplate(factsForOdu, oduName, scopeID, generationID,
		"eshu-fixture-assumes", assumesIAMRoleFamilyWorkloadID("b"), "b", "assumes-app-b")
	appendAssumesIAMRoleResource(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNA, "assumes-app-a")
	appendAssumesIAMRoleResource(factsForOdu, oduName, scopeID, generationID, assumesFamilyRoleARNB, "assumes-app-b")
}

// appendAssumesWorkloadPodTemplate appends the pod-template fact for one
// exact-chain lane's workload, keyed by the same workload object id the
// lane's identity-use fact carries so the committed node satisfies the
// readiness lookup uid-exactly. The lane letter seeds the synthetic image
// digest so the two lanes carry distinct refs; every lane letter in these
// families is a valid hex digit.
func appendAssumesWorkloadPodTemplate(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, clusterID, objectID, lane, appName string) {
	namespace := "default"
	groupVersionResource := "apps/v1/deployments"
	serviceAccount := appName
	containerName := appName + "-c0"
	imageRef := "ghcr.io/eshu-hq/eshu-fixture-" + appName + "@sha256:" + strings.Repeat(lane, 64)
	init := false
	selector := map[string]string{"app": appName}
	payload, err := factschema.EncodeKubernetesLivePodTemplate(kuberneteslivev1.PodTemplate{
		ObjectID:             objectID,
		ClusterID:            &clusterID,
		Namespace:            &namespace,
		Name:                 &appName,
		GroupVersionResource: &groupVersionResource,
		ServiceAccount:       &serviceAccount,
		Containers: []kuberneteslivev1.PodTemplateContainer{
			{
				Name:  &containerName,
				Image: &imageRef,
				Init:  &init,
			},
		},
		ImageRefs:          []string{imageRef},
		Selector:           selector,
		Labels:             selector,
		CorrelationAnchors: []string{objectID},
	})
	if err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: encode pod_template %q: %v", oduName, objectID, err))
	}
	*factsForOdu = append(*factsForOdu, facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         cloud.KubernetesPodTemplateFactKind,
		StableFactKey:    objectID,
		SchemaVersion:    cloud.KubernetesPodTemplateSchemaVersion,
		CollectorKind:    "kubernetes_live",
		SourceConfidence: "observed",
		Payload:          payload,
	})
}

// appendAssumesIAMRoleResource appends the aws_resource fact for one
// exact-chain lane's IAM role, with the resource_id the AWS resource
// projection hashes into the same CloudResource uid the trust chain
// resolves for the lane's assume edge.
func appendAssumesIAMRoleResource(factsForOdu *[]facts.Envelope, oduName, scopeID, generationID, roleARN, roleName string) {
	serviceKind := "iam"
	payload, err := factschema.EncodeAWSResource(awsv1.Resource{
		AccountID:          "123456789012",
		ResourceID:         roleARN,
		Region:             "us-east-1",
		ResourceType:       awsv1.ResourceTypeIAMRole,
		ARN:                &roleARN,
		Name:               &roleName,
		ServiceKind:        &serviceKind,
		CorrelationAnchors: []string{roleARN},
	})
	if err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: encode aws_resource %q: %v", oduName, roleARN, err))
	}
	*factsForOdu = append(*factsForOdu, facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         cloud.AWSResourceFactKind,
		StableFactKey:    fmt.Sprintf("aws:123456789012:us-east-1:%s:%s", awsv1.ResourceTypeIAMRole, roleARN),
		SchemaVersion:    cloud.AWSResourceSchemaVersion,
		CollectorKind:    "aws",
		SourceConfidence: "observed",
		Payload:          payload,
	})
}

// AssumesIAMRoleFamilyOdu builds the cataloged Odù for the
// assumes_iam_role direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanAssumeFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func AssumesIAMRoleFamilyOdu() CatalogOdu {
	const oduName = AssumesIAMRoleFamilyOduName
	const scopeID = assumesIAMRoleFamilyScopeID
	const generationID = assumesIAMRoleFamilyGenerationID
	factsForOdu := make([]facts.Envelope, 0, 41)

	appendAssumesLaneA(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesLaneB(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesLaneC(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesLaneD(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesLaneE(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesLaneF(&factsForOdu, oduName, scopeID, generationID)
	appendAssumesEndpointObservations(&factsForOdu, oduName, scopeID, generationID)

	return CatalogOdu{
		Odu:    Odu{Name: AssumesIAMRoleFamilyOduName, Facts: factsForOdu},
		Detail: "forty-one facts for the direct-materialization assumes_iam_role family: two exact chains with Vault hops across both identity mechanisms (IRSA web-identity and EKS Pod Identity) producing two edges, four deliberate non-producers covering the fingerprint-only, partial-chain, Deny, and wildcard-trust branches, lane A's second bound Vault role proving duplicate delivery converges on one edge, and four endpoint observations (pod templates for the exact lanes' workloads and aws_resource facts for the exact lanes' IAM roles) so the live projection's endpoint-readiness gate finds the cross-scope endpoints no other scope commits, so the expected set proves both IAM assumption and restraint",
	}
}
