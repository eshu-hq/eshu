// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// The iam_instance_profile_role family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.IAMInstanceProfileRoleEdgeWriter through the
// WriteIAMInstanceProfileRoleEdges port. The relationship type is HAS_ROLE,
// read off canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat's MERGE and
// the closed iamInstanceProfileRoleRelationshipVocabulary the writer screens
// each row against. It is NOT iamInstanceProfileRoleEdgeLabel
// ("IAM_INSTANCE_PROFILE_HAS_ROLE"), which is statement metadata carried
// beside the query rather than a graph relationship type — a second #6181-shaped
// trap on the same family, one level below the port name.
//
// Facts are built as typed awsv1.Resource values and encoded through
// factschema.EncodeAWSResource, never as hand-built maps (Contract System v1).
// The role_arns attribute sits one level deeper, under Attributes["attributes"],
// because that is where the awscloud IAM scanner emitter nests every
// scanner-provided attribute (#4633) and where
// awsv1.DecodeResourceIAMInstanceProfileAttributes reads it from. Writing it at
// the payload's top level instead would decode to an empty RoleARNs slice, and
// the fixture would produce zero edges while looking correct.

const (
	// IAMInstanceProfileRoleFamilyOduName is this Odù's catalog name, the ref
	// a materialized_edges:iam_instance_profile_role coverage row would name
	// to resolve through it.
	IAMInstanceProfileRoleFamilyOduName = "odu:ifa-iam-instance-profile-role-family"

	// iamInstanceProfileRoleFamilyScopeID is the single AWS account scope
	// every fact in this Odù belongs to. The reducer handler loads one scope
	// generation's aws_resource facts, so a fixture spanning scopes would not
	// mirror any real intent.
	iamInstanceProfileRoleFamilyScopeID = "aws:eshu-fixture-account"

	// iamInstanceProfileRoleFamilyAccountID and
	// iamInstanceProfileRoleFamilyRegion are two of the four inputs to the
	// CloudResource uid both edge endpoints are keyed by
	// (reducer.cloudResourceUID -> facts.StableID over account_id, region,
	// resource_id, resource_type). They are synthetic, matching the
	// collector's own redaction posture.
	iamInstanceProfileRoleFamilyAccountID = "123456789012"
	iamInstanceProfileRoleFamilyRegion    = "us-east-1"

	// iamInstanceProfileRoleFamilyGenerationID is the one scope generation the
	// committed cassette replays. The reducer's handler loads a single scope
	// generation's aws_resource facts, so every fact below shares it.
	iamInstanceProfileRoleFamilyGenerationID = "gen-ifa-iam-instance-profile-role-family-1"

	// iamInstanceProfileRoleFamilyCollectorKind and
	// iamInstanceProfileRoleFamilySchemaVersion mirror what the awscloud
	// collector stamps on an aws_resource fact, so the compiled Odù and the
	// committed cassette describe the same envelope rather than agreeing only
	// on the payload.
	iamInstanceProfileRoleFamilyCollectorKind = "aws"
	iamInstanceProfileRoleFamilySchemaVersion = "1.0.0"

	// iamInstanceProfileRoleFamilySourceConfidence marks these facts as
	// directly observed, the posture a scanner-emitted resource carries.
	iamInstanceProfileRoleFamilySourceConfidence = "observed"
)

// iamInstanceProfileRoleFamilyStableFactKey derives one fact's durable
// dedup key from the identity the collector would key it by, rather than
// letting the cassette and the compiled Odù carry two hand-typed strings that
// can drift apart.
func iamInstanceProfileRoleFamilyStableFactKey(resourceType, resourceID string) string {
	return fmt.Sprintf(
		"aws:%s:%s:iam:%s:%s",
		iamInstanceProfileRoleFamilyAccountID, iamInstanceProfileRoleFamilyRegion,
		resourceType, resourceID,
	)
}

// iamInstanceProfileRoleFixture describes one aws_resource fact in the Odù.
//
// RoleARNs is set only for instance profiles; a role fixture leaves it nil. The
// extractor reads it exclusively off the aws_iam_instance_profile side, so a
// role carrying it would be silently ignored rather than rejected.
type iamInstanceProfileRoleFixture struct {
	// ResourceType is the provider-defined type the extractor branches on:
	// aws_iam_role builds the join index, aws_iam_instance_profile drives
	// edge emission.
	ResourceType string
	// ResourceID is the provider-assigned id. It is the uid input, so it is
	// what the expected-edge fixture's endpoint identities are derived from.
	ResourceID string
	// ARN is the join key: the profile names roles by ARN, and the role side
	// of the index is keyed by ARN (and by ResourceID when the two differ).
	ARN string
	// RoleARNs are the role ARNs an instance profile declares, nested under
	// Attributes["attributes"]["role_arns"] on the wire.
	RoleARNs []string
}

// iamInstanceProfileRoleFamilyFixtures is the hand-authored aws_resource set
// this Odù carries: two roles, and three instance profiles that between them
// cover fan-out, unresolved targets, and the no-attachment case.
//
// The unresolved and no-attachment profiles are the load-bearing half. The
// extractor never fabricates an endpoint — an unmatched role ARN is counted as
// target_unresolved and dropped — and the writer's two MATCH clauses would
// no-op on a missing node anyway. Without them, a regression that started
// inventing a CloudResource for every named ARN would still reproduce the
// expected set exactly and this fixture would report green.
var iamInstanceProfileRoleFamilyFixtures = []iamInstanceProfileRoleFixture{
	{
		ResourceType: awsv1.ResourceTypeIAMRole,
		ResourceID:   "eshu-fixture-app-role",
		ARN:          "arn:aws:iam::123456789012:role/eshu-fixture-app-role",
	},
	{
		ResourceType: awsv1.ResourceTypeIAMRole,
		ResourceID:   "eshu-fixture-batch-role",
		ARN:          "arn:aws:iam::123456789012:role/eshu-fixture-batch-role",
	},
	{
		// FAN-OUT: one profile naming two scanned roles produces TWO edges.
		// A one-edge-per-profile regression is invisible to a fixture whose
		// profiles each attach a single role.
		ResourceType: awsv1.ResourceTypeIAMInstanceProfile,
		ResourceID:   "eshu-fixture-app-profile",
		ARN:          "arn:aws:iam::123456789012:instance-profile/eshu-fixture-app-profile",
		RoleARNs: []string{
			"arn:aws:iam::123456789012:role/eshu-fixture-app-role",
			"arn:aws:iam::123456789012:role/eshu-fixture-batch-role",
		},
	},
	{
		// UNRESOLVED TARGET: the named role was never scanned into this
		// generation, so no aws_iam_role fact exists for it and no edge may
		// be produced.
		ResourceType: awsv1.ResourceTypeIAMInstanceProfile,
		ResourceID:   "eshu-fixture-orphan-profile",
		ARN:          "arn:aws:iam::123456789012:instance-profile/eshu-fixture-orphan-profile",
		RoleARNs:     []string{"arn:aws:iam::123456789012:role/never-scanned-role"},
	},
	{
		// NO ATTACHMENT: a profile with an empty role_arns list. The
		// extractor returns before resolving anything, so this proves the
		// zero-attachment path emits nothing rather than an edge with a
		// blank target.
		ResourceType: awsv1.ResourceTypeIAMInstanceProfile,
		ResourceID:   "eshu-fixture-empty-profile",
		ARN:          "arn:aws:iam::123456789012:instance-profile/eshu-fixture-empty-profile",
	},
}

// IAMInstanceProfileRoleFamilyOdu builds the cataloged Odù for the
// iam_instance_profile_role direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason KubernetesNamespaceEnvironmentFamilyOdu does:
// a failure means the payload contract moved under a committed fixture, and
// every coverage claim built on it is already void.
func IAMInstanceProfileRoleFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(iamInstanceProfileRoleFamilyFixtures))
	for _, fixture := range iamInstanceProfileRoleFamilyFixtures {
		arn := fixture.ARN
		resource := awsv1.Resource{
			AccountID:    iamInstanceProfileRoleFamilyAccountID,
			ResourceID:   fixture.ResourceID,
			Region:       iamInstanceProfileRoleFamilyRegion,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
		}
		if fixture.ResourceType == awsv1.ResourceTypeIAMInstanceProfile {
			nested := map[string]any{}
			if len(fixture.RoleARNs) > 0 {
				roleARNs := make([]any, 0, len(fixture.RoleARNs))
				for _, roleARN := range fixture.RoleARNs {
					roleARNs = append(roleARNs, roleARN)
				}
				nested["role_arns"] = roleARNs
			} else {
				nested["role_arns"] = []any{}
			}
			resource.Attributes = map[string]any{"attributes": nested}
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"ifa: catalog_seed %s: encode aws_resource payload for %q: %v",
				IAMInstanceProfileRoleFamilyOduName, fixture.ResourceID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          iamInstanceProfileRoleFamilyScopeID,
			GenerationID:     iamInstanceProfileRoleFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    iamInstanceProfileRoleFamilyStableFactKey(fixture.ResourceType, fixture.ResourceID),
			SchemaVersion:    iamInstanceProfileRoleFamilySchemaVersion,
			CollectorKind:    iamInstanceProfileRoleFamilyCollectorKind,
			SourceConfidence: iamInstanceProfileRoleFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: IAMInstanceProfileRoleFamilyOduName, Facts: factsForOdu},
		Detail: "five aws_resource facts for the direct-materialization iam_instance_profile_role family: two IAM roles and three instance profiles covering two-role fan-out, an unresolved role ARN that must produce no edge, and a profile with no attachment",
	}
}
