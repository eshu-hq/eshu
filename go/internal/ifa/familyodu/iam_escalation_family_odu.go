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

// The iam_escalation family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.IAMEscalationEdgeWriter through the WriteIAMEscalationEdges port.
// The relationship type is CAN_ESCALATE_TO, the static token baked into
// canonicalIAMEscalationEdgeUpsertCypher (no %s: the merged primitive set
// lives in the rel.primitives property, never in the relationship type, so
// the MERGE keys on the stable (principal_uid, CAN_ESCALATE_TO, target_uid)
// triple). It Read it off the template and the iamEscalationEdgeLabel const
// (which IS "CAN_ESCALATE_TO": the const doubles as the relationship type
// and the statement-metadata tag), never by deriving from the port or family
// name — "IAM_ESCALATION" appears nowhere in code, and a name-derived
// literal would match no executed statement.
//
// Facts are built as typed awsv1.Resource / iamv1.Permission values and
// encoded through factschema.EncodeAWSResource /
// factschema.EncodeAWSIAMPermission, never as hand-built maps (Contract System
// v1). Actions are lowercase (the collector normalizes actions[] to
// lowercase; IAM actions are case-insensitive so this is loss-free).
//
// Scope note: identity-policy statements only. The escalation grant folds
// every Allow statement's trusted actions without a policy-source filter,
// but this Odù still uses inline statements throughout: a mixed-source
// fixture would assert nothing the extractor distinguishes, and the
// evaluation_scope honesty label belongs to the sibling iamcan families,
// not to this one. Permission boundaries, resource policies, SCPs, and
// session policies are outside this Odù's slice.

const (
	// IAMEscalationFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:iam_escalation coverage row would name to resolve
	// through it.
	IAMEscalationFamilyOduName = "odu:ifa-iam-escalation-family"

	// iamEscalationFamilyScopeID is the single AWS account scope every fact
	// in this Odù belongs to. The reducer handler loads one scope
	// generation's facts, so a fixture spanning scopes would not mirror any
	// real intent.
	//
	// The scope is deliberately NOT aws:eshu-fixture-account (owned by the
	// iam_instance_profile_role cassette), NOT
	// aws:eshu-fixture-can-assume-account (owned by the iam_can_assume
	// cassette), and NOT aws:eshu-fixture-can-perform-account (owned by the
	// iam_can_perform cassette). Scopes carry one ACTIVE generation; driving
	// a second generation into an occupied scope supersedes the first
	// family's generation, its handler never runs, and the sibling
	// exact-set assert fails with zero edges — diagnosed live 2026-09-27 on
	// the shared scope during the iam_can_assume drive. One live generation
	// per scope per cell is the contract.
	iamEscalationFamilyScopeID = "aws:eshu-fixture-iam-escalation-account"

	// iamEscalationFamilyAccountID is the account every fact below shares.
	// Synthetic, matching the collector's own redaction posture.
	iamEscalationFamilyAccountID = "123456789012"

	// iamEscalationFamilyIAMRegion is the region every IAM node fact carries.
	// IAM is a global service; the iam_can_assume Odù proved aws-global end
	// to end through the live drive, so this family mirrors it rather than
	// inventing a second convention. The join index folds region into the
	// node uid, so the statement's target ARN and the scanned node agree by
	// construction.
	iamEscalationFamilyIAMRegion = "aws-global"

	// iamEscalationFamilyGenerationID is the one scope generation the Odù
	// replays. The reducer's handler loads a single scope generation's
	// facts, so every fact below shares it.
	iamEscalationFamilyGenerationID = "gen-ifa-iam-escalation-family-1"

	// iamEscalationFamilyCollectorKind mirrors what the awscloud collector
	// stamps on both fact kinds, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	iamEscalationFamilyCollectorKind = "aws"

	// iamEscalationFamilySourceConfidence marks these facts as directly
	// observed, the posture a scanner-emitted resource carries.
	iamEscalationFamilySourceConfidence = "observed"
)

// The fixture principal and target ARNs. attacker is the user whose identity
// statements produce edges; victim is the second principal proving edges are
// not attacker-only as well as a user target. team-policy, exec-role,
// deploy-role, and admins are the scanned escalation targets across the
// policy, role, and group kinds; bystander is a scanned role with no
// statements that must never gain an edge on the strength of being scanned;
// ghost-unscanned names a user the scope never scanned, so a statement
// attached to it cannot anchor an edge; ghost-policy names a policy the
// scope never scanned, so a statement naming it resolves to nothing.
const (
	iamEscalationFamilyAttackerUserARN  = "arn:aws:iam::123456789012:user/attacker"
	iamEscalationFamilyVictimUserARN    = "arn:aws:iam::123456789012:user/victim"
	iamEscalationFamilyTeamPolicyARN    = "arn:aws:iam::123456789012:policy/team-policy"
	iamEscalationFamilyExecRoleARN      = "arn:aws:iam::123456789012:role/exec-role"
	iamEscalationFamilyDeployRoleARN    = "arn:aws:iam::123456789012:role/deploy-role"
	iamEscalationFamilyAdminsGroupARN   = "arn:aws:iam::123456789012:group/admins"
	iamEscalationFamilyBystanderRoleARN = "arn:aws:iam::123456789012:role/bystander"
	iamEscalationFamilyGhostUserARN     = "arn:aws:iam::123456789012:user/ghost-unscanned"
	iamEscalationFamilyGhostPolicyARN   = "arn:aws:iam::123456789012:policy/ghost-unscanned"
)

// iamEscalationFamilyResourceFixture describes one aws_resource fact in the
// Odù: an IAM principal node, an IAM target node, or a scanned node with no
// statements proving scanning alone never gains an edge.
type iamEscalationFamilyResourceFixture struct {
	// ResourceType is awsv1.ResourceTypeIAMUser,
	// awsv1.ResourceTypeIAMRole, awsv1.ResourceTypeIAMPolicy, or
	// awsv1.ResourceTypeIAMGroup. The resolver requires a matched node
	// classify as the primitive's expected type, so type accuracy is
	// load-bearing, not decorative.
	ResourceType string
	// ARN is both the resource_id and the ARN, the scanner convention this
	// family's index lookups key on.
	ARN string
}

// iamEscalationFamilyResources are the scanned node substrate: two edge
// principals (both users), four edge targets across the policy/role/group
// kinds, and one scanned role with no statements that must never gain an
// edge on the strength of being scanned.
var iamEscalationFamilyResources = []iamEscalationFamilyResourceFixture{
	{ResourceType: awsv1.ResourceTypeIAMUser, ARN: iamEscalationFamilyAttackerUserARN},
	{ResourceType: awsv1.ResourceTypeIAMUser, ARN: iamEscalationFamilyVictimUserARN},
	{ResourceType: awsv1.ResourceTypeIAMPolicy, ARN: iamEscalationFamilyTeamPolicyARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamEscalationFamilyExecRoleARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamEscalationFamilyDeployRoleARN},
	{ResourceType: awsv1.ResourceTypeIAMGroup, ARN: iamEscalationFamilyAdminsGroupARN},
	{ResourceType: awsv1.ResourceTypeIAMRole, ARN: iamEscalationFamilyBystanderRoleARN},
}

// iamEscalationFamilyPermissionFixture describes one aws_iam_permission
// identity statement in the Odù.
type iamEscalationFamilyPermissionFixture struct {
	// PrincipalARN is the IAM user the statement is attached to. A value no
	// scanned node carries proves the unscanned-principal branch drops the
	// whole statement.
	PrincipalARN string
	// PrincipalType is the collector's normalized principal resource type
	// ("user" throughout: every fixture principal is a user).
	PrincipalType string
	// Effect is "Allow" or "Deny". A Deny contributes to denyActions and
	// arms nothing.
	Effect string
	// PolicySource is "inline" throughout: this Odù exercises inline
	// identity statements only.
	PolicySource string
	// Actions are the statement's lowercase IAM actions. A complete
	// primitive's every action must be present in the principal's trusted
	// set for the primitive to arm.
	Actions []string
	// NotActions makes the statement non-trustable: a catalogued action it
	// carries counts skipped_not_action_resource instead of arming.
	NotActions []string
	// Resources are the statement's resource ARN patterns. An exact
	// scanned ARN of the expected IAM type resolves; "*" is ambiguous; an
	// unscanned ARN is unresolved; a scanned ARN of the wrong IAM type
	// fails the expected-type check.
	Resources []string
	// HasConditions with ConditionKeys marks a conditioned statement: a
	// catalogued action it carries counts skipped_conditioned, because
	// conditions carry key names only, never values. Keys name keys only,
	// never values (private-data boundary).
	HasConditions bool
	ConditionKeys []string
}

// iamEscalationFamilyPermissions are the identity statements: six
// edge-producing Allows converging on five edges (two primitives merging on
// one role edge — the idempotent-merge half of the proof — plus a PassRole
// edge and a second-principal edge), and nine deliberate non-producers
// covering the self-loop, deny, conditioned, NotAction, wildcard,
// unscanned-target, unscanned-principal, sts:AssumeRole-deferral, and
// wrong-target branches.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — a self-loop, a Deny, a conditioned statement, a
// NotAction, a wildcard, an unscanned ARN, an unscanned principal, a
// deferred AssumeRole, and a wrong-typed ARN each resolve to nothing — and
// the writer's two MATCH clauses would no-op on a missing node anyway.
// Without them, a regression that started emitting an edge for every armed
// primitive regardless of target resolution would still reproduce the
// expected set exactly and this fixture would report green.
var iamEscalationFamilyPermissions = []iamEscalationFamilyPermissionFixture{
	{
		// EDGE: iam:createpolicyversion on the exact policy ARN — the
		// policy-target primitive.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:createpolicyversion"},
		Resources:     []string{iamEscalationFamilyTeamPolicyARN},
	},
	{
		// EDGE: iam:attachrolepolicy on the exact role ARN — the
		// role-target primitive.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:attachrolepolicy"},
		Resources:     []string{iamEscalationFamilyExecRoleARN},
	},
	{
		// EDGES (merged): iam:putrolepolicy plus iam:updateassumerolepolicy
		// on the same exact role ARN converge on ONE edge with the merged
		// sorted primitive set — the idempotent-merge half of the proof.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:putrolepolicy", "iam:updateassumerolepolicy"},
		Resources:     []string{iamEscalationFamilyExecRoleARN},
	},
	{
		// EDGE: iam:addusertogroup on the exact group ARN — the
		// group-target primitive.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:addusertogroup"},
		Resources:     []string{iamEscalationFamilyAdminsGroupARN},
	},
	{
		// EDGE: the PassRole-family multi-action primitive — iam:passrole
		// plus BOTH service actions (the catalog's lambda primitive
		// requires createfunction AND invokefunction; two of three must
		// not arm), target read off the passrole statement's own
		// resources.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:passrole", "lambda:createfunction", "lambda:invokefunction"},
		Resources:     []string{iamEscalationFamilyDeployRoleARN},
	},
	{
		// EDGE: a second principal resolving a user-target primitive —
		// principals are not attacker-only and targets are not policy-only.
		PrincipalARN:  iamEscalationFamilyVictimUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:createaccesskey"},
		Resources:     []string{iamEscalationFamilyAttackerUserARN},
	},
	{
		// NO EDGE: iam:createaccesskey armed on the principal's own ARN is
		// a self-escalation — dropped silently, counted as neither edge
		// nor skip.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:createaccesskey"},
		Resources:     []string{iamEscalationFamilyAttackerUserARN},
	},
	{
		// NO EDGE: a Deny contributes to denyActions and arms nothing.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Deny",
		Actions:       []string{"iam:attachuserpolicy"},
		Resources:     []string{iamEscalationFamilyVictimUserARN},
	},
	{
		// NO EDGE: a conditioned statement — conditions carry key names
		// only, never values, so the grant cannot trust it.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		Effect:        "Allow",
		Actions:       []string{"iam:putuserpolicy"},
		Resources:     []string{iamEscalationFamilyVictimUserARN},
		HasConditions: true,
		ConditionKeys: []string{"aws:MultiFactorAuthPresent"},
	},
	{
		// NO EDGE: a NotAction statement is non-trustable even though it
		// names a catalogued-adjacent action.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:putgrouppolicy"},
		NotActions:    []string{"iam:deletegrouppolicy"},
		Resources:     []string{iamEscalationFamilyAdminsGroupARN},
	},
	{
		// NO EDGE: iam:updateloginprofile granted only on "*" names no
		// single node — ambiguous, never an edge.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:updateloginprofile"},
		Resources:     []string{"*"},
	},
	{
		// NO EDGE: iam:createloginprofile granted only on a well-formed
		// ARN the scope never scanned resolves to nothing rather than to
		// a fabricated node.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:createloginprofile"},
		Resources:     []string{iamEscalationFamilyGhostPolicyARN},
	},
	{
		// NO EDGE: the attached principal was never scanned, so the whole
		// statement is dropped and counted unresolved — it cannot anchor
		// an edge.
		PrincipalARN:  iamEscalationFamilyGhostUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:attachrolepolicy"},
		Resources:     []string{iamEscalationFamilyExecRoleARN},
	},
	{
		// NO EDGE: sts:AssumeRole is recognized and deferred to the
		// CAN_ASSUME trust edge — counted once per principal, never
		// emitted here.
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"sts:assumerole"},
		Resources:     []string{iamEscalationFamilyExecRoleARN},
	},
	{
		// NO EDGE: iam:createaccesskey is a user-target primitive, but the
		// named ARN classifies as a role, not a user — the expected-type
		// check refuses the cross-type match. (The victim's
		// iam:createaccesskey on the attacker user ARN still resolves
		// through its own statement; this pattern contributes no edge of
		// its own.)
		PrincipalARN:  iamEscalationFamilyAttackerUserARN,
		PrincipalType: "user",
		PolicySource:  "inline",
		Effect:        "Allow",
		Actions:       []string{"iam:createaccesskey"},
		Resources:     []string{iamEscalationFamilyExecRoleARN},
	},
}

// iamEscalationFamilyStableFactKey derives one fact's durable dedup key from
// the same identity inputs the collector keys its StableID by, in a readable
// fixture-local format rather than a hex digest, instead of hand-typing a
// string the expected-edge fixture cannot independently check.
//
// The permission half MUST include actions and resources, not just
// principal:effect: several fixture statements share
// attacker:Allow prefixes and differ only in actions/resources (self-loop vs
// producers, wildcard vs exact, wrong-type vs exact, ghost policy vs team
// policy), so keying on principal alone would collapse distinct statements:
// live ingest would drive fewer facts than claimed and negative controls
// would never reach the extractor. Key narrowness is a silent live-coverage
// loss the offline guard cannot see (it runs over all compiled facts), so
// the derivation below carries every collector identity input the fixture
// varies.
func iamEscalationFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		iamEscalationFamilyAccountID,
		factKind, identity,
	)
}

// IAMEscalationFamilyOdu builds the cataloged Odù for the iam_escalation
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMCanPerformFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func IAMEscalationFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(iamEscalationFamilyResources)+len(iamEscalationFamilyPermissions))
	for _, fixture := range iamEscalationFamilyResources {
		arn := fixture.ARN
		resource := awsv1.Resource{
			AccountID:    iamEscalationFamilyAccountID,
			ResourceID:   fixture.ARN,
			Region:       iamEscalationFamilyIAMRegion,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				IAMEscalationFamilyOduName, fixture.ARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamEscalationFamilyScopeID,
			GenerationID:     iamEscalationFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    iamEscalationFamilyStableFactKey(facts.AWSResourceFactKind, fixture.ARN),
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    iamEscalationFamilyCollectorKind,
			SourceConfidence: iamEscalationFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range iamEscalationFamilyPermissions {
		principalType := fixture.PrincipalType
		permission := iamv1.Permission{
			AccountID:     iamEscalationFamilyAccountID,
			Region:        iamEscalationFamilyIAMRegion,
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
				IAMEscalationFamilyOduName, fixture.PrincipalARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamEscalationFamilyScopeID,
			GenerationID:     iamEscalationFamilyGenerationID,
			FactKind:         facts.AWSIAMPermissionFactKind,
			StableFactKey:    iamEscalationFamilyStableFactKey(facts.AWSIAMPermissionFactKind, fixture.PrincipalARN+":"+fixture.Effect+":"+joinEscalationKeyParts(fixture.Actions)+":"+joinEscalationKeyParts(fixture.NotActions)+":"+joinEscalationKeyParts(fixture.Resources)+":"+joinEscalationKeyParts(fixture.ConditionKeys)),
			SchemaVersion:    facts.AWSIAMPermissionSchemaVersion,
			CollectorKind:    iamEscalationFamilyCollectorKind,
			SourceConfidence: iamEscalationFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: IAMEscalationFamilyOduName, Facts: factsForOdu},
		Detail: "twenty-two facts for the direct-materialization iam_escalation family: seven aws_resource node facts (two edge principals, four escalation targets across policy/role/group, one scanned role with no statements) and fifteen aws_iam_permission identity statements (six edge-producing Allows converging on five CAN_ESCALATE_TO edges with two primitives merging on one role edge, nine deliberate non-producers covering the self-loop, deny, conditioned, NotAction, wildcard, unscanned-target, unscanned-principal, sts:AssumeRole-deferral, and wrong-target branches), so the CAN_ESCALATE_TO expected set proves both primitive resolution and restraint",
	}
}

// joinEscalationKeyParts joins a string slice for stable-fact-key derivation.
// The fixture Actions are already sorted lowercase, so the key is stable
// without a second sort.
func joinEscalationKeyParts(values []string) string {
	key := ""
	for i, value := range values {
		if i > 0 {
			key += ","
		}
		key += value
	}
	return key
}
