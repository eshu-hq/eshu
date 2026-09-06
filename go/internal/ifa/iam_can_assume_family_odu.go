// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// The iam_can_assume family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.IAMCanAssumeEdgeWriter through the WriteIAMCanAssumeEdges port. The
// relationship type is CAN_ASSUME, read off
// canonicalIAMCanAssumeEdgeUpsertCypherFormat's MERGE after the closed
// iamCanAssumeRelationshipVocabulary token is substituted. It is NOT
// iamCanAssumeEdgeLabel ("IAM_CAN_ASSUME"), which is statement metadata
// carried beside the query rather than a graph relationship type — the same
// #6181-shaped trap one level below the port name that
// iam_instance_profile_role_family_odu.go documents for HAS_ROLE.
//
// Facts are built as typed awsv1.Resource / iamv1.Permission values and
// encoded through factschema.EncodeAWSResource /
// factschema.EncodeAWSIAMPermission, never as hand-built maps (Contract System
// v1). The resource type constants come from awsv1 rather than string
// literals, so a contract rename moves this fixture with it instead of
// silently describing resources the join index no longer recognises.

const (
	// IAMCanAssumeFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:iam_can_assume coverage row would name to resolve
	// through it.
	IAMCanAssumeFamilyOduName = "odu:ifa-iam-can-assume-family"

	// iamCanAssumeFamilyScopeID is the single AWS account scope every fact in
	// this Odù belongs to. The reducer handler loads one scope generation's
	// aws_resource + aws_iam_permission facts, so a fixture spanning scopes
	// would not mirror any real intent.
	iamCanAssumeFamilyScopeID = "aws:eshu-fixture-account"

	// iamCanAssumeFamilyAccountID and iamCanAssumeFamilyRegion are the account
	// and region every fact below shares. IAM is a global service, so the
	// region is "aws-global" and each resource_id is its ARN, matching the
	// iam scanner's roleObservation / userObservation. They are synthetic,
	// matching the collector's own redaction posture.
	iamCanAssumeFamilyAccountID = "123456789012"
	iamCanAssumeFamilyRegion    = "aws-global"

	// iamCanAssumeFamilyGenerationID is the one scope generation the Odù
	// replays. The reducer's handler loads a single scope generation's facts,
	// so every fact below shares it.
	iamCanAssumeFamilyGenerationID = "gen-ifa-iam-can-assume-family-1"

	// iamCanAssumeFamilyCollectorKind mirrors what the awscloud collector
	// stamps on both fact kinds, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	iamCanAssumeFamilyCollectorKind = "aws"

	// iamCanAssumeFamilySourceConfidence marks these facts as directly
	// observed, the posture a scanner-emitted resource carries.
	iamCanAssumeFamilySourceConfidence = "observed"
)

// The fixture principal ARNs. eshu-runtime is the role-with-trust-policy;
// ci-deployer and breakglass are the two scanned assuming principals that
// produce edges. unattached-observer scans as a role node but carries no
// trust statement, ghost-unscanned names a role the scope never scanned, and
// foreign-deployer lives in an account whose roles were never scanned here.
const (
	iamCanAssumeFamilyRuntimeRoleARN    = "arn:aws:iam::123456789012:role/eshu-runtime"
	iamCanAssumeFamilyDeployerRoleARN   = "arn:aws:iam::123456789012:role/ci-deployer"
	iamCanAssumeFamilyBreakglassUserARN = "arn:aws:iam::123456789012:user/breakglass"
	iamCanAssumeFamilyObserverRoleARN   = "arn:aws:iam::123456789012:role/unattached-observer"
	iamCanAssumeFamilyGhostRoleARN      = "arn:aws:iam::123456789012:role/ghost-unscanned"
	iamCanAssumeFamilyForeignRoleARN    = "arn:aws:iam::999988887777:role/foreign-deployer"
)

// iamCanAssumeFamilyResourceFixture describes one aws_resource fact in the
// Odù: an IAM role/user node for the join index, or a deliberate
// non-principal proving the index ignores it.
type iamCanAssumeFamilyResourceFixture struct {
	// ResourceType is awsv1.ResourceTypeIAMRole, awsv1.ResourceTypeIAMUser,
	// or a non-principal type. Only the first two enter the join index.
	ResourceType string
	// ARN is both the resource_id and the ARN, the iam scanner's convention
	// for IAM resources.
	ARN string
}

// iamCanAssumeFamilyResources are the scanned node substrate: three edge
// endpoints, one scanned role with no trust statement (it must never gain an
// edge on the strength of being scanned), and one non-principal resource the
// join index must ignore.
var iamCanAssumeFamilyResources = []iamCanAssumeFamilyResourceFixture{
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamCanAssumeFamilyRuntimeRoleARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamCanAssumeFamilyDeployerRoleARN},
	{ResourceType: awsv1.ResourceTypeIAMUser, ARN: iamCanAssumeFamilyBreakglassUserARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamCanAssumeFamilyObserverRoleARN},
	{ResourceType: awsv1.ResourceTypeS3Bucket, ARN: "arn:aws:s3:::eshu-fixture-artifacts"},
}

// iamCanAssumeFamilyPermissionFixture describes one aws_iam_permission fact
// in the Odù.
type iamCanAssumeFamilyPermissionFixture struct {
	// PrincipalARN is the role-with-trust-policy the statement is attached
	// to. A value no scanned role carries proves the source-unresolved
	// branch resolves to nothing.
	PrincipalARN string
	// Effect is "Allow" or "Deny". A Deny trust statement grants no assume
	// and must produce no edge.
	Effect string
	// PolicySource is "trust" for trust statements. Any other source is
	// silently skipped by the extractor, whatever its effect.
	PolicySource string
	// AssumePrincipals are the trust statement's candidate principals. Each
	// entry exercises one resolution branch: scanned role, scanned user,
	// wildcard, AWS-service principal, unscanned foreign ARN, or the role
	// itself.
	AssumePrincipals []string
}

// iamCanAssumeFamilyPermissions are the trust statements: one edge-producing
// Allow with a role and a user principal, and seven deliberate non-producers
// covering the deny, wildcard, service-principal, external-unresolved,
// source-unresolved, self-assume, and non-trust-source branches.
//
// The non-producers are the load-bearing half. The extractor never fabricates
// an endpoint — a wildcard, a service principal, an unscanned ARN, a Deny,
// and a self-assume each resolve to nothing — and the writer's two MATCH
// clauses would no-op on a missing node anyway. Without them, a regression
// that started inventing a CloudResource for every named principal would
// still reproduce the expected set exactly and this fixture would report
// green.
var iamCanAssumeFamilyPermissions = []iamCanAssumeFamilyPermissionFixture{
	{
		// EDGES: two scanned principals trusted by the runtime role, one
		// role and one user so the principal_kind dimension proves both.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{iamCanAssumeFamilyDeployerRoleARN, iamCanAssumeFamilyBreakglassUserARN},
	},
	{
		// NO EDGE: a Deny trust statement grants no assume-role.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Deny",
		PolicySource:     "trust",
		AssumePrincipals: []string{iamCanAssumeFamilyDeployerRoleARN},
	},
	{
		// NO EDGE: a wildcard principal names no concrete node.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{"*"},
	},
	{
		// NO EDGE: an AWS-service principal is not an ARN and resolves to
		// no scanned role/user node.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{"ec2.amazonaws.com"},
	},
	{
		// NO EDGE: a well-formed ARN whose account was never scanned in
		// this scope resolves to nothing rather than to a fabricated node.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{iamCanAssumeFamilyForeignRoleARN},
	},
	{
		// NO EDGE: the role-with-trust-policy itself was never scanned, so
		// the whole statement cannot anchor an edge.
		PrincipalARN:     iamCanAssumeFamilyGhostRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{iamCanAssumeFamilyDeployerRoleARN},
	},
	{
		// NO EDGE: a self-assume carries no trust truth.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "trust",
		AssumePrincipals: []string{iamCanAssumeFamilyRuntimeRoleARN},
	},
	{
		// NO EDGE: a non-trust source is silently skipped whatever its
		// effect and principals.
		PrincipalARN:     iamCanAssumeFamilyRuntimeRoleARN,
		Effect:           "Allow",
		PolicySource:     "inline",
		AssumePrincipals: []string{iamCanAssumeFamilyDeployerRoleARN},
	},
}

// iamCanAssumeFamilyStableFactKey derives one fact's durable dedup key from
// the identity the collector would key it by, rather than hand-typing a
// string the expected-edge fixture cannot independently check.
func iamCanAssumeFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:%s:%s:%s",
		iamCanAssumeFamilyAccountID, iamCanAssumeFamilyRegion,
		factKind, identity,
	)
}

// IAMCanAssumeFamilyOdu builds the cataloged Odù for the iam_can_assume
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMInstanceProfileRoleFamilyOdu does: a failure
// means the payload contract moved under a committed fixture, and every
// coverage claim built on it is already void.
func IAMCanAssumeFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(iamCanAssumeFamilyResources)+len(iamCanAssumeFamilyPermissions))
	for _, fixture := range iamCanAssumeFamilyResources {
		arn := fixture.ARN
		resource := awsv1.Resource{
			AccountID:    iamCanAssumeFamilyAccountID,
			ResourceID:   fixture.ARN,
			Region:       iamCanAssumeFamilyRegion,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"ifa: catalog_seed %s: encode aws_resource payload for %q: %v",
				IAMCanAssumeFamilyOduName, fixture.ARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamCanAssumeFamilyScopeID,
			GenerationID:     iamCanAssumeFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    iamCanAssumeFamilyStableFactKey(facts.AWSResourceFactKind, fixture.ARN),
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    iamCanAssumeFamilyCollectorKind,
			SourceConfidence: iamCanAssumeFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range iamCanAssumeFamilyPermissions {
		permission := iamv1.Permission{
			AccountID:        iamCanAssumeFamilyAccountID,
			Region:           iamCanAssumeFamilyRegion,
			PrincipalARN:     fixture.PrincipalARN,
			Effect:           fixture.Effect,
			PolicySource:     fixture.PolicySource,
			AssumePrincipals: fixture.AssumePrincipals,
		}
		payload, err := factschema.EncodeAWSIAMPermission(permission)
		if err != nil {
			panic(fmt.Sprintf(
				"ifa: catalog_seed %s: encode aws_iam_permission payload for %q: %v",
				IAMCanAssumeFamilyOduName, fixture.PrincipalARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamCanAssumeFamilyScopeID,
			GenerationID:     iamCanAssumeFamilyGenerationID,
			FactKind:         facts.AWSIAMPermissionFactKind,
			StableFactKey:    iamCanAssumeFamilyStableFactKey(facts.AWSIAMPermissionFactKind, fixture.PrincipalARN+":"+fixture.PolicySource+":"+fixture.Effect+":"+strings.Join(fixture.AssumePrincipals, ",")),
			SchemaVersion:    facts.AWSIAMPermissionSchemaVersion,
			CollectorKind:    iamCanAssumeFamilyCollectorKind,
			SourceConfidence: iamCanAssumeFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: IAMCanAssumeFamilyOduName, Facts: factsForOdu},
		Detail: "thirteen facts for the direct-materialization iam_can_assume family: five aws_resource node facts (three edge endpoints, one scanned role with no trust statement, one non-principal the join index ignores) and eight aws_iam_permission trust statements (one Allow producing two edges across both principal kinds, seven deliberate non-producers covering the deny, wildcard, service-principal, external-unresolved, source-unresolved, self-assume, and non-trust-source branches), so the CAN_ASSUME expected set proves both trust resolution and restraint",
	}
}
