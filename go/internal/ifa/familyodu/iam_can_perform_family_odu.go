// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// The iam_can_perform family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.IAMCanPerformEdgeWriter through the WriteIAMCanPerformEdges port.
// The relationship type is CAN_PERFORM, the static token baked into
// CanonicalIAMCanPerformEdgeUpsertCypher (no %s: the granted action set lives
// in the rel.actions property, never in the relationship type, so the MERGE
// keys on the stable (principal_uid, CAN_PERFORM, resource_uid) triple). It
// Read it off the template and the iamCanPerformEdgeLabel const (which IS
// "CAN_PERFORM": the const doubles as the relationship type and the
// statement-metadata tag), never by deriving from the port or family name —
// "IAM_CAN_PERFORM" appears nowhere in code, and a name-derived literal would
// match no executed statement. (iam_can_assume genuinely has the split this
// family does not: its label is "IAM_CAN_ASSUME", distinct from CAN_ASSUME.)
//
// Facts are built as typed awsv1.Resource / iamv1.Permission values and
// encoded through factschema.EncodeAWSResource /
// factschema.EncodeAWSIAMPermission, never as hand-built maps (Contract System
// v1). Actions are lowercase (the collector normalizes actions[] to
// lowercase; IAM actions are case-insensitive so this is loss-free) and must
// be members of the closed iamcan catalog, or the extractor counts the
// statement skipped_uncatalogued_action instead of resolving it.
//
// Scope note: identity-policy statements only. Resource-policy facts
// (aws_resource_policy_permission), permission boundaries, SCPs, condition
// values, and session policies are outside this Odù's slice — the extractor
// evaluates them on separate paths, and the rel.evaluation_scope honesty
// label (identity_policy_only here) says exactly that. A fixture mixing
// sources would assert an evaluation_scope the identity path never stamps.

const (
	// IAMCanPerformFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:iam_can_perform coverage row would name to resolve
	// through it.
	IAMCanPerformFamilyOduName = "odu:ifa-iam-can-perform-family"

	// iamCanPerformFamilyScopeID is the single AWS account scope every fact in
	// this Odù belongs to. The reducer handler loads one scope generation's
	// facts, so a fixture spanning scopes would not mirror any real intent.
	//
	// The scope is deliberately NOT aws:eshu-fixture-account (owned by the
	// iam_instance_profile_role cassette) and NOT
	// aws:eshu-fixture-can-assume-account (owned by the iam_can_assume
	// cassette). Scopes carry one ACTIVE generation; driving a second
	// generation into an occupied scope supersedes the first family's
	// generation, its handler never runs, and the sibling exact-set assert
	// fails with zero edges — diagnosed live 2026-09-27 on the shared scope
	// during the iam_can_assume drive. One live generation per scope per
	// cell is the contract.
	iamCanPerformFamilyScopeID = "aws:eshu-fixture-can-perform-account"

	// iamCanPerformFamilyAccountID is the account every fact below shares.
	// Synthetic, matching the collector's own redaction posture.
	iamCanPerformFamilyAccountID = "123456789012"

	// iamCanPerformFamilyIAMRegion is the region IAM principal facts carry.
	// IAM is a global service; the iam_can_assume Odù proved aws-global
	// end to end through the live drive, so this family mirrors it rather
	// than inventing a second convention.
	iamCanPerformFamilyIAMRegion = "aws-global"

	// iamCanPerformFamilyDataRegion is the region data-plane resource facts
	// carry. S3/KMS/DynamoDB are regional services; us-east-1 mirrors the
	// scanner convention for regional resources. The join index folds region
	// into the node uid, so the statement's resource ARN and the scanned
	// node agree by construction — the extractor resolves by ARN equality
	// against scanned nodes, and region digits never branch it.
	iamCanPerformFamilyDataRegion = "us-east-1"

	// iamCanPerformFamilyGenerationID is the one scope generation the Odù
	// replays. The reducer's handler loads a single scope generation's facts,
	// so every fact below shares it.
	iamCanPerformFamilyGenerationID = "gen-ifa-iam-can-perform-family-1"

	// iamCanPerformFamilyCollectorKind mirrors what the awscloud collector
	// stamps on both fact kinds, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	iamCanPerformFamilyCollectorKind = "aws"

	// iamCanPerformFamilySourceConfidence marks these facts as directly
	// observed, the posture a scanner-emitted resource carries.
	iamCanPerformFamilySourceConfidence = "observed"
)

// The fixture principal ARNs. deployer is the user whose identity statements
// produce edges; ci-role is the role proving role principals resolve too.
// unattached-observer scans as a role node but carries no statement, and
// ghost-unscanned names a user the scope never scanned, so a statement
// attached to it cannot anchor an edge.
const (
	iamCanPerformFamilyDeployerUserARN = "arn:aws:iam::123456789012:user/deployer"
	iamCanPerformFamilyCIRoleARN       = "arn:aws:iam::123456789012:role/ci-role"
	iamCanPerformFamilyObserverRoleARN = "arn:aws:iam::123456789012:role/unattached-observer"
	iamCanPerformFamilyGhostUserARN    = "arn:aws:iam::123456789012:user/ghost-unscanned"
)

// The fixture target ARNs. reports is the S3 bucket two catalog actions
// converge on (one edge, merged sorted action set); vault-key is the KMS key
// proving a second service family resolves on the same principal;
// orders-table is the DynamoDB table proving role principals resolve.
// ghost-docs is never scanned, so a statement naming it resolves to nothing.
const (
	iamCanPerformFamilyReportsBucketARN = "arn:aws:s3:::eshu-fixture-reports"
	iamCanPerformFamilyVaultKeyARN      = "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	iamCanPerformFamilyOrdersTableARN   = "arn:aws:dynamodb:us-east-1:123456789012:table/eshu-fixture-orders"
	iamCanPerformFamilyGhostBucketARN   = "arn:aws:s3:::eshu-fixture-ghost"
)

// iamCanPerformFamilyResourceFixture describes one aws_resource fact in the
// Odù: an IAM principal node, a data-plane target node, or a scanned node
// with no statements proving scanning alone never gains an edge.
type iamCanPerformFamilyResourceFixture struct {
	// ResourceType is awsv1.ResourceTypeIAMUser, awsv1.ResourceTypeIAMRole,
	// or one of the catalog's data-plane types. The resolver requires a
	// matched node classify as the catalog entry's expected type, so type
	// accuracy is load-bearing, not decorative.
	ResourceType string
	// Region is iamCanPerformFamilyIAMRegion for IAM nodes and
	// iamCanPerformFamilyDataRegion for regional resources; it folds into
	// the node uid, so both sides of a resolution agree by construction.
	Region string
	// ARN is both the resource_id and the ARN, the scanner convention this
	// family's index lookups key on.
	ARN string
}

// iamCanPerformFamilyResources are the scanned node substrate: two edge
// principals (user + role), three edge targets across three service
// families, and one scanned role with no statements that must never gain an
// edge on the strength of being scanned.
var iamCanPerformFamilyResources = []iamCanPerformFamilyResourceFixture{
	{ResourceType: awsv1.ResourceTypeIAMUser, Region: iamCanPerformFamilyIAMRegion, ARN: iamCanPerformFamilyDeployerUserARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, Region: iamCanPerformFamilyIAMRegion, ARN: iamCanPerformFamilyCIRoleARN},
	{ResourceType: awsv1.ResourceTypeS3Bucket, Region: iamCanPerformFamilyDataRegion, ARN: iamCanPerformFamilyReportsBucketARN},
	{ResourceType: awsv1.ResourceTypeKMSKey, Region: iamCanPerformFamilyDataRegion, ARN: iamCanPerformFamilyVaultKeyARN},
	{ResourceType: awsv1.ResourceTypeDynamoDBTable, Region: iamCanPerformFamilyDataRegion, ARN: iamCanPerformFamilyOrdersTableARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, Region: iamCanPerformFamilyIAMRegion, ARN: iamCanPerformFamilyObserverRoleARN},
}

// iamCanPerformFamilyPermissionFixture describes one aws_iam_permission
// identity statement in the Odù.
type iamCanPerformFamilyPermissionFixture struct {
	// PrincipalARN is the IAM user/role the statement is attached to. A
	// value no scanned node carries proves the unscanned-principal branch
	// resolves to nothing.
	PrincipalARN string
	// PrincipalType is the collector's normalized principal resource type.
	PrincipalType string
	// Effect is "Allow" or "Deny". A Deny contributes to denyActions and
	// arms nothing.
	Effect string
	// PolicySource is "inline" or "attached_managed" for identity statements
	// the perform grant folds in; any other source is skipped by the grant
	// builder whatever its effect.
	PolicySource string
	// Actions are the statement's lowercase IAM actions. Catalogued actions
	// arm the grant; uncatalogued ones count skipped_uncatalogued_action.
	Actions []string
	// NotActions makes the statement non-trustable: a catalogued action it
	// carries counts skipped_not_action_resource instead of arming.
	NotActions []string
	// Resources are the statement's resource ARN patterns. An exact scanned
	// ARN resolves exact_arn; "*" is ambiguous; an unscanned ARN is
	// unresolved; a scanned ARN of the wrong service family fails the
	// catalog's expected-type check.
	Resources []string
	// HasConditions with ConditionKeys marks a conditioned statement: a
	// catalogued action it carries counts skipped_conditioned, because
	// conditions carry key names only, never values. Keys name keys only,
	// never values (private-data boundary).
	HasConditions bool
	ConditionKeys []string
}

// iamCanPerformFamilyPermissions are the identity statements: three
// edge-producing Allows (two catalog actions converging on one S3 edge, one
// KMS action, one DynamoDB action on the role principal) and ten deliberate
// non-producers covering the type-mismatch, deny, conditioned, NotAction,
// uncatalogued, wildcard, unscanned-target, non-identity-source,
// unscanned-principal, and wrong-target branches.
//
// The non-producers are the load-bearing half. The extractor never fabricates
// an endpoint — a type-mismatched ARN, a Deny, a conditioned statement, a
// wildcard, an unscanned ARN, and an unscanned principal each resolve to
// nothing — and the writer's two MATCH clauses would no-op on a missing node
// anyway. Without them, a regression that started inventing a CloudResource
// for every named ARN would still reproduce the expected set exactly and
// this fixture would report green.
var iamCanPerformFamilyPermissions = []iamCanPerformFamilyPermissionFixture{
	{
		// EDGES: two catalog actions on the exact bucket ARN converge on
		// ONE edge with the merged sorted action set — the idempotent-merge
		// half of the proof.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject", "s3:putobject"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// EDGE: a KMS catalog action on the exact key ARN — the second
		// service family on the same principal.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"kms:decrypt"},
		Resources:     []string{iamCanPerformFamilyVaultKeyARN},
	},
	{
		// EDGE: a role principal resolving a DynamoDB catalog action —
		// principals are not user-only.
		PrincipalARN:  iamCanPerformFamilyCIRoleARN,
		PrincipalType: "role",
		Effect:        "Allow",
		PolicySource:  "attached_managed",
		Actions:       []string{"dynamodb:getitem"},
		Resources:     []string{iamCanPerformFamilyOrdersTableARN},
	},
	{
		// NO EDGE: s3:getobject is catalogued, but the named ARN classifies
		// as a KMS key, not an S3 bucket — the catalog's expected-type
		// check refuses the cross-type match. (The s3:getobject action as
		// a whole still resolves through the exact bucket match from the
		// edge-producing statement; this pattern contributes no edge of
		// its own.)
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject"},
		Resources:     []string{iamCanPerformFamilyVaultKeyARN},
	},
	{
		// NO EDGE: a Deny contributes to denyActions and arms nothing.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Deny",
		PolicySource:  "inline",
		Actions:       []string{"s3:deletebucket"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// NO EDGE: a conditioned statement — conditions carry key names
		// only, never values, so the grant cannot trust it.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:listbucket"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
		HasConditions: true,
		ConditionKeys: []string{"aws:MultiFactorAuthPresent"},
	},
	{
		// NO EDGE: a NotAction statement is non-trustable even though it
		// carries a catalogued action.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject"},
		NotActions:    []string{"s3:deletebucket"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// NO EDGE: iam:passrole is outside the closed catalog — an
		// out-of-vocabulary action has no closed target semantics.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"iam:passrole"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// NO EDGE: a bare "*" names no single node — ambiguous, never an
		// edge. (The s3:getobject "*" here would be absorbed by the exact
		// bucket match from the edge-producing statement anyway; the
		// standalone ambiguous proof is the lambda statement below, whose
		// action no exact statement grants.)
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject"},
		Resources:     []string{"*"},
	},
	{
		// NO EDGE: lambda:invokefunction is catalogued and trusted, but its
		// only resource pattern is "*" — with no exact statement granting
		// the action, the wildcard names no single node.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"lambda:invokefunction"},
		Resources:     []string{"*"},
	},
	{
		// NO EDGE: a well-formed ARN the scope never scanned resolves to
		// nothing rather than to a fabricated node.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject"},
		Resources:     []string{iamCanPerformFamilyGhostBucketARN},
	},
	{
		// NO EDGE: a trust-source statement is not an identity policy — the
		// perform grant folds inline and attached_managed only.
		PrincipalARN:  iamCanPerformFamilyDeployerUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "trust",
		Actions:       []string{"s3:getobject"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// NO EDGE: the attached principal was never scanned, so the whole
		// statement cannot anchor an edge.
		PrincipalARN:  iamCanPerformFamilyGhostUserARN,
		PrincipalType: "user",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"s3:getobject"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
	{
		// NO EDGE: ec2:terminateinstances is catalogued, but the bucket is
		// not an EC2 instance — catalogued-action/wrong-target, the mirror
		// of the KMS type-mismatch above in the compute direction.
		PrincipalARN:  iamCanPerformFamilyCIRoleARN,
		PrincipalType: "role",
		Effect:        "Allow",
		PolicySource:  "inline",
		Actions:       []string{"ec2:terminateinstances"},
		Resources:     []string{iamCanPerformFamilyReportsBucketARN},
	},
}

// iamCanPerformFamilyStableFactKey derives one fact's durable dedup key from
// the same identity inputs the collector keys its StableID by, in a readable
// fixture-local format rather than a hex digest, instead of hand-typing a
// string the expected-edge fixture cannot independently check.
//
// The permission half MUST include resources, not-actions, and condition
// keys, not just principal:source:effect:actions: the collector's stable
// identity (NewIAMPermissionEnvelope in
// go/internal/collector/awscloud/iam_permission_envelope.go) folds actions,
// effect, not_actions, not_resources, policy coordinates, principal, region,
// resources, statement SID, and the condition summary into StableID, and the
// replay ingest upserts ON CONFLICT (fact_id) with fact_id derived from
// (scope, generation, stable_fact_key). A narrower key collapses distinct
// statements: four fixture statements share deployer:inline:Allow:s3:getobject
// and differ only in resources/not-actions (KMS type-mismatch, NotAction,
// wildcard, ghost bucket), so keying on actions alone drives 17 facts live
// instead of the claimed 20, and three negative controls never reach the
// extractor. Key narrowness is a silent live-coverage loss the offline guard
// cannot see (it runs over all 20 facts), so the derivation below carries
// every collector identity input the fixture varies.
func iamCanPerformFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		iamCanPerformFamilyAccountID,
		factKind, identity,
	)
}

// IAMCanPerformFamilyOdu builds the cataloged Odù for the iam_can_perform
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanAssumeFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func IAMCanPerformFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(iamCanPerformFamilyResources)+len(iamCanPerformFamilyPermissions))
	for _, fixture := range iamCanPerformFamilyResources {
		arn := fixture.ARN
		resource := awsv1.Resource{
			AccountID:    iamCanPerformFamilyAccountID,
			ResourceID:   fixture.ARN,
			Region:       fixture.Region,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				IAMCanPerformFamilyOduName, fixture.ARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamCanPerformFamilyScopeID,
			GenerationID:     iamCanPerformFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    iamCanPerformFamilyStableFactKey(facts.AWSResourceFactKind, fixture.ARN),
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    iamCanPerformFamilyCollectorKind,
			SourceConfidence: iamCanPerformFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range iamCanPerformFamilyPermissions {
		principalType := fixture.PrincipalType
		permission := iamv1.Permission{
			AccountID:     iamCanPerformFamilyAccountID,
			Region:        iamCanPerformFamilyIAMRegion,
			PrincipalARN:  fixture.PrincipalARN,
			PrincipalType: &principalType,
			Effect:        fixture.Effect,
			PolicySource:  fixture.PolicySource,
			Actions:       fixture.Actions,
			NotActions:    fixture.NotActions,
			Resources:     fixture.Resources,
		}
		if fixture.HasConditions {
			hasConditions := true
			permission.HasConditions = &hasConditions
			permission.ConditionKeys = fixture.ConditionKeys
		}
		payload, err := factschema.EncodeAWSIAMPermission(permission)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_iam_permission payload for %q: %v",
				IAMCanPerformFamilyOduName, fixture.PrincipalARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamCanPerformFamilyScopeID,
			GenerationID:     iamCanPerformFamilyGenerationID,
			FactKind:         facts.AWSIAMPermissionFactKind,
			StableFactKey:    iamCanPerformFamilyStableFactKey(facts.AWSIAMPermissionFactKind, fixture.PrincipalARN+":"+fixture.PolicySource+":"+fixture.Effect+":"+joinCanPerformKeyParts(fixture.Actions)+":"+joinCanPerformKeyParts(fixture.NotActions)+":"+joinCanPerformKeyParts(fixture.Resources)+":"+joinCanPerformKeyParts(fixture.ConditionKeys)),
			SchemaVersion:    facts.AWSIAMPermissionSchemaVersion,
			CollectorKind:    iamCanPerformFamilyCollectorKind,
			SourceConfidence: iamCanPerformFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: IAMCanPerformFamilyOduName, Facts: factsForOdu},
		Detail: "twenty facts for the direct-materialization iam_can_perform family: six aws_resource node facts (two edge principals across user+role kinds, three data-plane targets across S3/KMS/DynamoDB, one scanned role with no statements) and fourteen aws_iam_permission identity statements (three edge-producing Allows converging on three edges, eleven deliberate non-producers covering the type-mismatch, deny, conditioned, NotAction, uncatalogued, absorbed-wildcard, standalone-wildcard, unscanned-target, non-identity-source, unscanned-principal, and wrong-target branches), so the CAN_PERFORM expected set proves both grant resolution and restraint",
	}
}

// joinCanPerformKeyParts joins a string slice for stable-fact-key derivation.
// The fixture Actions are already sorted lowercase, so the key is stable
// without a second sort.
func joinCanPerformKeyParts(values []string) string {
	key := ""
	for i, value := range values {
		if i > 0 {
			key += ","
		}
		key += value
	}
	return key
}
