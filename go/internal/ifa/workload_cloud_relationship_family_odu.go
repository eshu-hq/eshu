// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// The workload_cloud_relationship family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.WorkloadCloudRelationshipWriter through the
// WriteWorkloadCloudRelationshipEdges port. The relationship type is USES,
// read off workloadCloudRelationshipUpsertCypherFormat's MERGE and the closed
// workloadCloudRelationshipVocabulary the writer screens each row against. It
// is NOT workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE"),
// which is statement metadata carried beside the query rather than a graph
// relationship type — a second #6181-shaped trap on the same family, one level
// below the port name, exactly the shape
// iam_instance_profile_role_family_odu.go documents for HAS_ROLE.
//
// Facts are built as typed awsv1.Resource values and encoded through
// factschema.EncodeAWSResource, never as hand-built maps (Contract System v1).
// The workload anchor sits at the payload's TOP level (Attributes
// ["workload_id"/"workload_ids"], ["environment"], ["service_name"]), because
// that is where awsv1.DecodeResourceAnchorAttributes reads it. The iam
// family's role_arns live one level deeper under Attributes["attributes"]
// because the awscloud IAM scanner nests scanner-provided attributes there;
// copying that nesting here would decode to zero workload anchors, and the
// fixture would produce zero edges while looking correct.

const (
	// WorkloadCloudRelationshipFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:workload_cloud_relationship coverage row would
	// name to resolve through it.
	WorkloadCloudRelationshipFamilyOduName = "odu:ifa-workload-cloud-relationship-family"

	// workloadCloudRelationshipFamilyScopeID is the single AWS account scope
	// every fact in this Odù belongs to. The reducer handler loads one scope
	// generation's aws_resource facts, so a fixture spanning scopes would not
	// mirror any real intent.
	workloadCloudRelationshipFamilyScopeID = "aws:eshu-fixture-account"

	// workloadCloudRelationshipFamilyAccountID and
	// workloadCloudRelationshipFamilyRegion are two of the four inputs to the
	// CloudResource uid the edge target is keyed by
	// (reducer.cloudResourceUIDForResource -> payloadcore.CloudResourceUID
	// over account_id, region, resource_type, resource_id). They are
	// synthetic, matching the collector's own redaction posture.
	workloadCloudRelationshipFamilyAccountID = "123456789012"
	workloadCloudRelationshipFamilyRegion    = "us-east-1"

	// workloadCloudRelationshipFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's aws_resource facts, so every fact below shares it.
	workloadCloudRelationshipFamilyGenerationID = "gen-ifa-workload-cloud-relationship-family-1"

	// workloadCloudRelationshipFamilyCollectorKind and
	// workloadCloudRelationshipFamilySchemaVersion mirror what the awscloud
	// collector stamps on an aws_resource fact, so the compiled Odù describes
	// the same envelope a live generation would carry rather than agreeing
	// only on the payload.
	workloadCloudRelationshipFamilyCollectorKind = "aws"
	workloadCloudRelationshipFamilySchemaVersion = "1.0.0"

	// workloadCloudRelationshipFamilySourceConfidence marks these facts as
	// directly observed, the posture a scanner-emitted resource carries.
	workloadCloudRelationshipFamilySourceConfidence = "observed"
)

// workloadCloudRelationshipFamilyStableFactKey derives one fact's durable
// dedup key from the identity the collector would key it by, rather than
// hand-typing a string the expected-edge fixture cannot independently check.
func workloadCloudRelationshipFamilyStableFactKey(resourceType, resourceID string) string {
	return fmt.Sprintf(
		"aws:%s:%s:%s:%s",
		workloadCloudRelationshipFamilyAccountID, workloadCloudRelationshipFamilyRegion,
		resourceType, resourceID,
	)
}

// workloadCloudRelationshipFixture describes one aws_resource fact in the Odù.
//
// WorkloadIDs, ServiceNames and Environment are stamped onto the typed
// resource's top-level Attributes, which EncodeAWSResource merges back to
// top-level payload keys — the exact surface
// awsv1.DecodeResourceAnchorAttributes reads.
type workloadCloudRelationshipFixture struct {
	// ResourceType is the provider-defined type. It is a uid input, so it is
	// what the expected-edge fixture's target identities are derived from.
	ResourceType string
	// ResourceID is the provider-assigned id. It is the other uid input; ARN
	// is left nil on purpose so the uid derives from the id path, not the
	// ARN-fallback path of cloudResourceUIDForResource.
	ResourceID string
	// WorkloadIDs anchors this resource to workloads. Zero means unanchored,
	// one means an exact anchor, two means ambiguous (no edge either way).
	WorkloadIDs []string
	// PluralSpelling emits workload_ids as a single-element list even for a
	// single anchor, proving the plural attributeStringUnion spelling on the
	// edge path. Without it every one-element fixture would exercise only
	// the scalar spelling and a regression breaking list-form decoding
	// would stay green.
	PluralSpelling bool
	// ServiceNames is the service-name anchor. Present on the first fixture
	// to prove the workload+service source/reason pair, absent on the second
	// to prove the workload-only pair, and deliberately alone on the third
	// to prove a service-only anchor stays candidate evidence.
	ServiceNames []string
	// Environment is the instance environment. Empty means the anchor cannot
	// resolve to a WorkloadInstance, so no edge even with a workload id.
	Environment string
}

// workloadCloudRelationshipFamilyFixtures is the hand-authored aws_resource
// set this Odù carries: two edge-producing resources and three deliberate
// non-producers covering the service-only, ambiguous-anchor and
// missing-environment skip branches.
//

// The non-producers are the load-bearing half. The extractor never fabricates
// an endpoint — an ambiguous anchor, a missing environment, a service-only
// anchor and a malformed anchor each resolve to nothing — and the writer's
// two MATCH clauses would no-op on a missing node anyway. Without them, a
// regression that started inventing a CloudResource for every named workload
// would still reproduce the expected set exactly and this fixture would
// report green.
var workloadCloudRelationshipFamilyFixtures = []workloadCloudRelationshipFixture{
	{
		// EDGE: workload + service + environment. The anchor decision source
		// is payload.workload_id+service_name with reason
		// explicit_workload_and_service_anchor.
		ResourceType: "aws_ssm_parameter",
		ResourceID:   "/config/orders-api/database-url",
		WorkloadIDs:  []string{"workload:orders-api"},
		ServiceNames: []string{"orders-api"},
		Environment:  "prod",
	},
	{
		// EDGE: workload-only anchor through the plural key spelling, proving
		// both attributeStringUnion spellings. Source payload.workload_ids
		// (single-element list form), reason explicit_workload_anchor.
		ResourceType: "aws_sqs_queue",
		ResourceID:   "https://sqs.us-east-1.amazonaws.com/123456789012/orders-events",
		WorkloadIDs:  []string{"workload:orders-api"},
		// PluralSpelling keeps the list form the live scanner would carry
		// rather than collapsing to the scalar the builder defaults to.
		PluralSpelling: true,
		Environment:    "prod",
	},
	{
		// NO EDGE: service-name-only anchor. It stays candidate evidence and
		// is never promoted to graph truth.
		ResourceType: "aws_sns_topic",
		ResourceID:   "arn:aws:sns:us-east-1:123456789012:orders-events",
		ServiceNames: []string{"orders-api"},
		Environment:  "prod",
	},
	{
		// NO EDGE: two workload ids are ambiguous. The extractor must not
		// pick a representative.
		ResourceType: "aws_dynamodb_table",
		ResourceID:   "orders",
		WorkloadIDs:  []string{"workload:billing-api", "workload:orders-api"},
		Environment:  "prod",
	},
	{
		// NO EDGE: an exact workload anchor with no environment cannot
		// resolve to a WorkloadInstance (the writer MATCHes
		// instance.environment = row.environment).
		ResourceType: "aws_s3_bucket",
		ResourceID:   "eshu-fixture-artifacts",
		WorkloadIDs:  []string{"workload:orders-api"},
	},
}

// WorkloadCloudRelationshipFamilyOdu builds the cataloged Odù for the
// workload_cloud_relationship direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason IAMInstanceProfileRoleFamilyOdu does: a failure
// means the payload contract moved under a committed fixture, and every
// coverage claim built on it is already void.
func WorkloadCloudRelationshipFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(workloadCloudRelationshipFamilyFixtures))
	for _, fixture := range workloadCloudRelationshipFamilyFixtures {
		attrs := map[string]any{}
		if len(fixture.WorkloadIDs) > 1 || (len(fixture.WorkloadIDs) == 1 && fixture.PluralSpelling) {
			ids := make([]any, 0, len(fixture.WorkloadIDs))
			for _, id := range fixture.WorkloadIDs {
				ids = append(ids, id)
			}
			attrs["workload_ids"] = ids
		} else if len(fixture.WorkloadIDs) == 1 {
			attrs["workload_id"] = fixture.WorkloadIDs[0]
		}
		if len(fixture.ServiceNames) > 0 {
			attrs["service_name"] = fixture.ServiceNames[0]
		}
		if fixture.Environment != "" {
			attrs["environment"] = fixture.Environment
		}
		resource := awsv1.Resource{
			AccountID:    workloadCloudRelationshipFamilyAccountID,
			ResourceID:   fixture.ResourceID,
			Region:       workloadCloudRelationshipFamilyRegion,
			ResourceType: fixture.ResourceType,
			Attributes:   attrs,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"ifa: catalog_seed %s: encode aws_resource payload for %q: %v",
				WorkloadCloudRelationshipFamilyOduName, fixture.ResourceID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          workloadCloudRelationshipFamilyScopeID,
			GenerationID:     workloadCloudRelationshipFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    workloadCloudRelationshipFamilyStableFactKey(fixture.ResourceType, fixture.ResourceID),
			SchemaVersion:    workloadCloudRelationshipFamilySchemaVersion,
			CollectorKind:    workloadCloudRelationshipFamilyCollectorKind,
			SourceConfidence: workloadCloudRelationshipFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: WorkloadCloudRelationshipFamilyOduName, Facts: factsForOdu},
		Detail: "five aws_resource facts for the direct-materialization workload_cloud_relationship family: two edge-producing anchors (workload+service and workload-only, the latter through the plural key spelling) and three deliberate non-producers covering the service-only, ambiguous-anchor and missing-environment branches, so the USES expected set proves both promotion and restraint",
	}
}
