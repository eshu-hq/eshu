// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// The ec2_uses_profile family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.EC2UsesProfileEdgeWriter through the WriteEC2UsesProfileEdges port.
// The relationship type is USES_PROFILE, the single member of the writer's
// closed vocabulary substituted into the canonical upsert template's one
// %s; the MERGE keys on the stable (source_uid, USES_PROFILE, target_uid)
// triple. Read it off the template and the ec2UsesProfileRelationshipType
// const, never by deriving from the port or family name —
// "EC2_USES_PROFILE" appears nowhere in code, and a name-derived literal
// would match no executed statement.
//
// Facts are built as typed awsv1.Resource / awsv1.EC2InstancePosture values
// and encoded through factschema.EncodeAWSResource /
// factschema.EncodeEC2InstancePosture, never as hand-built maps (Contract
// System v1).
//
// Scope note: EC2 instance nodes materialize from the posture facts
// themselves (PR-A ec2_instance_node_materialization), so this Odù carries
// no aws_ec2_instance aws_resource facts — only the three scanned IAM
// instance-profile node facts the join resolves against, plus six posture
// facts. A ghost profile ARN names a profile the scope never scanned; the
// idle profile scans with no posture referencing it, proving scanning alone
// never gains an edge.

const (
	// EC2UsesProfileFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:ec2_uses_profile coverage row would name to resolve
	// through it.
	EC2UsesProfileFamilyOduName = "odu:ifa-ec2-uses-profile-family"

	// ec2UsesProfileFamilyScopeID is the single AWS account scope every fact
	// in this Odù belongs to. The reducer handler loads one scope
	// generation's facts, so a fixture spanning scopes would not mirror any
	// real intent.
	//
	// The scope is deliberately NOT aws:eshu-fixture-account (owned by the
	// iam_instance_profile_role cassette), NOT
	// aws:eshu-fixture-can-assume-account (owned by the iam_can_assume
	// cassette), NOT aws:eshu-fixture-can-perform-account (owned by the
	// iam_can_perform cassette), and NOT
	// aws:eshu-fixture-iam-escalation-account (owned by the iam_escalation
	// cassette). Scopes carry one ACTIVE generation; driving a second
	// generation into an occupied scope supersedes the first family's
	// generation, its handler never runs, and the sibling exact-set assert
	// fails with zero edges — diagnosed live 2026-09-27 on the shared scope
	// during the iam_can_assume drive. One live generation per scope per
	// cell is the contract.
	ec2UsesProfileFamilyScopeID = "aws:eshu-fixture-ec2-uses-profile-account"

	// ec2UsesProfileFamilyAccountID is the account every fact below shares.
	// Synthetic, matching the collector's own redaction posture.
	ec2UsesProfileFamilyAccountID = "123456789012"

	// ec2UsesProfileFamilyProfileRegion is the region every IAM
	// instance-profile node fact carries. IAM is a global service; the
	// iam_can_assume Odù proved aws-global end to end through the live
	// drive, so this family mirrors it rather than inventing a second
	// convention. The join index folds region into the node uid, so the
	// posture's profile ARN and the scanned node agree by construction.
	ec2UsesProfileFamilyProfileRegion = "aws-global"

	// ec2UsesProfileFamilyInstanceRegion is the region every EC2 posture
	// fact carries. EC2 is a regional service; us-east-1 mirrors what the
	// aws collector stamps.
	ec2UsesProfileFamilyInstanceRegion = "us-east-1"

	// ec2UsesProfileFamilyGenerationID is the one scope generation the Odù
	// replays. The reducer's handler loads a single scope generation's
	// facts, so every fact below shares it.
	ec2UsesProfileFamilyGenerationID = "gen-ifa-ec2-uses-profile-family-1"

	// ec2UsesProfileFamilyCollectorKind mirrors what the awscloud collector
	// stamps on both fact kinds, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	ec2UsesProfileFamilyCollectorKind = "aws"

	// ec2UsesProfileFamilySourceConfidence marks these facts as directly
	// observed, the posture a scanner-emitted resource carries.
	ec2UsesProfileFamilySourceConfidence = "observed"
)

// The fixture instance-profile ARNs. app-profile and batch-profile are the
// scanned edge targets; idle-profile scans with no posture referencing it,
// proving a scanned node alone never gains an edge; ghost-profile names a
// profile the scope never scanned, so a posture naming it resolves to
// nothing (target_unresolved, the conservative skip).
const (
	ec2UsesProfileFamilyAppProfileARN   = "arn:aws:iam::123456789012:instance-profile/app-profile"
	ec2UsesProfileFamilyBatchProfileARN = "arn:aws:iam::123456789012:instance-profile/batch-profile"
	ec2UsesProfileFamilyIdleProfileARN  = "arn:aws:iam::123456789012:instance-profile/idle-profile"
	ec2UsesProfileFamilyGhostProfileARN = "arn:aws:iam::999988887777:instance-profile/ghost-profile"
)

// ec2UsesProfileFamilyProfileFixture describes one aws_resource fact in the
// Odù: a scanned IAM instance-profile node the USES_PROFILE join resolves
// instance_profile_arn values against. resource_id equals the ARN, the
// scanner convention the profile index keys on.
type ec2UsesProfileFamilyProfileFixture struct {
	// ProfileARN is both the resource_id and the ARN.
	ProfileARN string
}

// ec2UsesProfileFamilyProfiles are the scanned node substrate: two edge
// targets and one scanned profile with no posture that must never gain an
// edge on the strength of being scanned.
var ec2UsesProfileFamilyProfiles = []ec2UsesProfileFamilyProfileFixture{
	{ProfileARN: ec2UsesProfileFamilyAppProfileARN},
	{ProfileARN: ec2UsesProfileFamilyBatchProfileARN},
	{ProfileARN: ec2UsesProfileFamilyIdleProfileARN},
}

// ec2UsesProfileFamilyPostureFixture describes one ec2_instance_posture fact
// in the Odù.
type ec2UsesProfileFamilyPostureFixture struct {
	// InstanceID is the bare EC2 instance id. Empty only where the source
	// is keyed by ARN (legacy inventory): the reducer keys the source uid
	// on the full ARN.
	InstanceID string
	// InstanceARN is the instance ARN. Empty only together with a set
	// InstanceID; both blank would fail the collector's own validation, so
	// the source_unresolved branch stays covered by the reducer's unit
	// tests rather than by an unemittable fixture member.
	InstanceARN string
	// ProfileARN is the attached IAM instance-profile ARN. Blank means no
	// profile is attached: the normal no-edge state, not a skip.
	ProfileARN string
	// Tombstone marks a terminated instance: no row and no skip.
	Tombstone bool
}

// ec2UsesProfileFamilyPostures are the instances: two edge producers (one by
// instance id, one by ARN fallback proving the legacy-inventory path), one
// bare instance with no profile, one terminated instance, and one instance
// naming an unscanned profile (the conservative skip).
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — a blank profile, a terminated instance, and an
// unscanned profile ARN each resolve to nothing — and the writer's two
// MATCH clauses would no-op on a missing node anyway. Without them, a
// regression that started emitting an edge for every posture regardless of
// target resolution would still reproduce the expected set exactly and this
// fixture would report green.
var ec2UsesProfileFamilyPostures = []ec2UsesProfileFamilyPostureFixture{
	{
		// EDGE: instance id resolves the source, app-profile resolves the
		// target (instance resolution mode).
		InstanceID:  "i-0aaa1111",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0aaa1111",
		ProfileARN:  ec2UsesProfileFamilyAppProfileARN,
	},
	{
		// EDGE: second instance converging on the batch profile.
		InstanceID:  "i-0bbb2222",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0bbb2222",
		ProfileARN:  ec2UsesProfileFamilyBatchProfileARN,
	},
	{
		// NO EDGE: blank profile ARN — a bare instance, not a lost edge,
		// and not a skip.
		InstanceID:  "i-0ccc3333",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0ccc3333",
		ProfileARN:  "",
	},
	{
		// NO EDGE: terminated instance — no row and no skip.
		InstanceID:  "i-0ddd4444",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0ddd4444",
		ProfileARN:  ec2UsesProfileFamilyAppProfileARN,
		Tombstone:   true,
	},
	{
		// NO EDGE: ghost profile never scanned — target_unresolved, the
		// conservative skip. Cross-account ARN proves the join is by exact
		// ARN, not by profile name.
		InstanceID:  "i-0eee5555",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0eee5555",
		ProfileARN:  ec2UsesProfileFamilyGhostProfileARN,
	},
	{
		// EDGE: blank instance id with only the ARN observed — the source
		// uid keys on the full ARN (arn resolution mode) and joins the
		// batch profile.
		InstanceID:  "",
		InstanceARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0fff6666",
		ProfileARN:  ec2UsesProfileFamilyBatchProfileARN,
	},
}

// ec2UsesProfileFamilyStableFactKey derives one fact's durable dedup key
// from the same identity inputs the collector keys its StableID by, in a
// readable fixture-local format rather than a hex digest, instead of
// hand-typing a string the expected-edge fixture cannot independently
// check.
//
// The posture half MUST include the instance identity, not just the
// profile: several fixture postures share profile ARNs and differ only in
// instance (edge producers vs bare vs terminated vs ghost), so keying on
// profile alone would collapse distinct postures: live ingest would drive
// fewer facts than claimed and negative controls would never reach the
// extractor. Key narrowness is a silent live-coverage loss the offline
// guard cannot see (it runs over all compiled facts), so the derivation
// below carries every collector identity input the fixture varies — the
// ARN where the instance id is blank.
func ec2UsesProfileFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		ec2UsesProfileFamilyAccountID,
		factKind, identity,
	)
}

// ec2UsesProfileFamilyPostureIdentity returns the identity input the stable
// key carries for one posture: the instance id, or the ARN where the id is
// blank (the legacy-inventory shape).
func ec2UsesProfileFamilyPostureIdentity(fixture ec2UsesProfileFamilyPostureFixture) string {
	if fixture.InstanceID != "" {
		return fixture.InstanceID
	}
	return fixture.InstanceARN
}

// EC2UsesProfileFamilyOdu builds the cataloged Odù for the ec2_uses_profile
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMEscalationFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func EC2UsesProfileFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(ec2UsesProfileFamilyProfiles)+len(ec2UsesProfileFamilyPostures))
	for _, fixture := range ec2UsesProfileFamilyProfiles {
		arn := fixture.ProfileARN
		resource := awsv1.Resource{
			AccountID:    ec2UsesProfileFamilyAccountID,
			ResourceID:   fixture.ProfileARN,
			Region:       ec2UsesProfileFamilyProfileRegion,
			ResourceType: awsv1.ResourceTypeIAMInstanceProfile,
			ARN:          &arn,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				EC2UsesProfileFamilyOduName, fixture.ProfileARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          ec2UsesProfileFamilyScopeID,
			GenerationID:     ec2UsesProfileFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    ec2UsesProfileFamilyStableFactKey(facts.AWSResourceFactKind, fixture.ProfileARN),
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    ec2UsesProfileFamilyCollectorKind,
			SourceConfidence: ec2UsesProfileFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range ec2UsesProfileFamilyPostures {
		instanceID := fixture.InstanceID
		instanceARN := fixture.InstanceARN
		profileARN := fixture.ProfileARN
		state := "running"
		resourceType := awsv1.ResourceTypeEC2Instance
		posture := awsv1.EC2InstancePosture{
			AccountID:          ec2UsesProfileFamilyAccountID,
			Region:             ec2UsesProfileFamilyInstanceRegion,
			InstanceID:         &instanceID,
			ARN:                &instanceARN,
			ResourceType:       &resourceType,
			State:              &state,
			InstanceProfileARN: &profileARN,
		}
		payload, err := factschema.EncodeEC2InstancePosture(posture)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode ec2_instance_posture payload for %q: %v",
				EC2UsesProfileFamilyOduName, ec2UsesProfileFamilyPostureIdentity(fixture), err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          ec2UsesProfileFamilyScopeID,
			GenerationID:     ec2UsesProfileFamilyGenerationID,
			FactKind:         facts.EC2InstancePostureFactKind,
			StableFactKey:    ec2UsesProfileFamilyStableFactKey(facts.EC2InstancePostureFactKind, ec2UsesProfileFamilyPostureIdentity(fixture)),
			SchemaVersion:    facts.EC2InstancePostureSchemaVersionV1,
			CollectorKind:    ec2UsesProfileFamilyCollectorKind,
			SourceConfidence: ec2UsesProfileFamilySourceConfidence,
			IsTombstone:      fixture.Tombstone,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: EC2UsesProfileFamilyOduName, Facts: factsForOdu},
		Detail: "nine facts for the direct-materialization ec2_uses_profile family: three aws_resource instance-profile node facts (two edge targets plus one scanned profile with no posture) and six ec2_instance_posture facts (two edge producers by instance id, one bare instance with no profile, one terminated instance, one instance naming an unscanned profile, and one ARN-keyed instance proving the legacy-inventory fallback), so the USES_PROFILE expected set proves both source-resolution modes and restraint",
	}
}
