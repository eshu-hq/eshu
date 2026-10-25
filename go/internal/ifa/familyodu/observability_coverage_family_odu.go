// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// The observability_coverage family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.ObservabilityCoverageEdgeWriter through the
// WriteObservabilityCoverageEdges port. The relationship types are the five
// AWS_COVERS_<signal> tokens, one per member of the writer's closed
// coverage-signal vocabulary (alarm, composite_alarm, dashboard, log_group,
// trace_sampling), each substituted into the canonical upsert template's one
// %s; the MERGE keys on its two endpoint nodes alone (both are MATCHed,
// never merged). Read them off the template plus the
// observabilityCoverageSignalVocabulary the write path screens each row
// against, never by deriving from the port or family name.
//
// Facts are built as typed awsv1.Resource / awsv1.Relationship values and
// encoded through factschema.EncodeAWSResource /
// factschema.EncodeAWSRelationship, never as hand-built maps (Contract
// System v1).
//
// Scope note: coverage resolves aws_resource observability objects against
// scanned CloudResource targets through aws_relationship dimension facts,
// so this Odù carries five exact pairs (one object plus one
// cloudwatch_alarm_observes_metric relationship each, one per signal),
// five scanned target resources, and three restraint pairs proving the
// classifier never fabricates an edge: a metric-name-only alarm (no
// resolvable dimension, rejected), an X-Ray rule matching by service name
// (derived provenance-only, never an edge), and an alarm naming an unscanned
// target (unresolved gap, never an edge).
//
// The orders EC2 target additionally carries its ec2_instance_posture fact:
// the EC2 node commits ONLY from that fact kind (aws_ec2_instance
// aws_resource facts never produce node rows, #5448 CRUX-1), so without it
// the live alarm row resolves against no node (first fault-shard run: 4/5
// edges). The real collector emits it from the same DescribeInstances pass,
// so the fixture mirrors a real generation.

const (
	// ObservabilityCoverageFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:observability_coverage coverage row would
	// name to resolve through it.
	ObservabilityCoverageFamilyOduName = "odu:ifa-observability-coverage-family"

	// observabilityCoverageFamilyScopeID is the single AWS account scope
	// every fact in this Odù belongs to. The reducer handler loads one
	// scope generation's facts, so a fixture spanning scopes would not
	// mirror any real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges. One live generation per scope per cell is the contract.
	observabilityCoverageFamilyScopeID = "aws:eshu-fixture-observability-coverage"

	// observabilityCoverageFamilyAccountID is the account every fact below
	// shares. Synthetic, matching the collector's own redaction posture.
	observabilityCoverageFamilyAccountID = "123456789012"

	// observabilityCoverageFamilyRegion is the region every fact below
	// carries. The aws collector stamps us-east-1 on these facts; the join
	// index folds region into the node uid, so objects, relationships, and
	// targets agree by construction.
	observabilityCoverageFamilyRegion = "us-east-1"

	// observabilityCoverageFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	observabilityCoverageFamilyGenerationID = "gen-ifa-observability-coverage-family-1"

	// observabilityCoverageFamilyCollectorKind mirrors what the aws
	// collector stamps on both fact kinds, so the Odù describes the same
	// envelopes a live generation would carry rather than agreeing only on
	// the payload.
	observabilityCoverageFamilyCollectorKind = "aws"

	// observabilityCoverageFamilySourceConfidence marks these facts as
	// directly observed, the posture a scanner-emitted resource carries.
	observabilityCoverageFamilySourceConfidence = "observed"
)

// observabilityCoverageFamilyObjectFixture describes one aws_resource
// observability-object fact in the Odù: an alarm, composite alarm,
// dashboard, log group, or X-Ray sampling rule the classifier maps to its
// coverage signal by resource type.
type observabilityCoverageFamilyObjectFixture struct {
	// Name is the fixture-local object name (an alarm name, dashboard
	// name, log group path, or rule name).
	Name string
	// ResourceType is the AWS resource type, which decides the coverage
	// signal (aws_cloudwatch_alarm, aws_cloudwatch_composite_alarm,
	// aws_cloudwatch_dashboard, aws_cloudwatch_logs_log_group, or
	// aws_xray_sampling_rule).
	ResourceType string
	// ARN is the object's ARN. The object ref the relationship source
	// resolves against is the ARN, so every relationship below names its
	// object's ARN as its source.
	ARN string
}

// observabilityCoverageFamilyObjects are the eight observability objects:
// five edge producers (one per signal) plus three restraints (a
// metric-name-only alarm, a service-name X-Ray rule, and an alarm naming an
// unscanned target).
var observabilityCoverageFamilyObjects = []observabilityCoverageFamilyObjectFixture{
	{
		// EDGE (alarm): orders-cpu-high observes the orders EC2
		// instance by ARN dimension.
		Name:         "orders-cpu-high",
		ResourceType: "aws_cloudwatch_alarm",
		ARN:          "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
	},
	{
		// EDGE (composite_alarm): checkout-rollup observes the orders
		// Lambda by ARN dimension.
		Name:         "checkout-rollup",
		ResourceType: "aws_cloudwatch_composite_alarm",
		ARN:          "arn:aws:cloudwatch:us-east-1:123456789012:alarm:checkout-rollup",
	},
	{
		// EDGE (dashboard): orders-overview observes the orders
		// database by bare-id dimension (the bare-id resolution
		// mode, proving the mode pin is not a vacuous constant).
		Name:         "orders-overview",
		ResourceType: "aws_cloudwatch_dashboard",
		ARN:          "arn:aws:cloudwatch:us-east-1:123456789012:dashboard/orders-overview",
	},
	{
		// EDGE (log_group): /orders/api observes the orders queue by
		// ARN dimension.
		Name:         "/orders/api",
		ResourceType: "aws_cloudwatch_logs_log_group",
		ARN:          "arn:aws:logs:us-east-1:123456789012:log-group:/orders/api",
	},
	{
		// EDGE (trace_sampling): the orders sampling rule observes the
		// orders table by ARN dimension. The relationship verb is the
		// same cloudwatch_alarm_observes_metric the classifier resolves
		// for every signal: the classifier keys relationships by source
		// ref alone, never by object signal, so a dimension-bearing
		// relationship sourced at the rule's ARN classifies exact with
		// the rule's own trace_sampling signal.
		Name:         "orders-rule",
		ResourceType: "aws_xray_sampling_rule",
		ARN:          "arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule",
	},
	{
		// NO EDGE: metric-name-only alarm. Its relationship carries no
		// dimensions, so the classifier rejects it rather than
		// fabricating an edge from a bare metric name.
		Name:         "orders-metric-only",
		ResourceType: "aws_cloudwatch_alarm",
		ARN:          "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-metric-only",
	},
	{
		// NO EDGE: service-name X-Ray rule. It matches by service name,
		// which is real but inferred coverage, so the classifier keeps
		// it derived and provenance-only, never an edge.
		Name:         "checkout-service-rule",
		ResourceType: "aws_xray_sampling_rule",
		ARN:          "arn:aws:xray:us-east-1:123456789012:sampling-rule/checkout-service-rule",
	},
	{
		// NO EDGE: alarm naming an unscanned target. The dimension
		// resolves to no scanned resource, so the classifier records an
		// unresolved gap rather than an edge.
		Name:         "orders-ghost-target",
		ResourceType: "aws_cloudwatch_alarm",
		ARN:          "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-ghost-target",
	},
}

// observabilityCoverageFamilyTargetFixture describes one scanned monitored
// resource: an exact edge's target endpoint.
type observabilityCoverageFamilyTargetFixture struct {
	// Name is the fixture-local target name.
	Name string
	// ResourceType is the AWS resource type (never an observability
	// type, so targets never enter the object index themselves).
	ResourceType string
	// ResourceID is the bare resource identity.
	ResourceID string
	// ARN is the target ARN.
	ARN string
}

// observabilityCoverageFamilyTargets are the five scanned targets, one per
// edge. The ghost target the restraint alarm names is deliberately absent:
// it was never scanned.
var observabilityCoverageFamilyTargets = []observabilityCoverageFamilyTargetFixture{
	{
		Name:         "orders-ec2",
		ResourceType: "aws_ec2_instance",
		ResourceID:   "i-0abc123def4567890",
		ARN:          "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
	},
	{
		Name:         "orders-lambda",
		ResourceType: "aws_lambda_function",
		ResourceID:   "orders-api",
		ARN:          "arn:aws:lambda:us-east-1:123456789012:function:orders-api",
	},
	{
		Name:         "orders-db",
		ResourceType: "aws_rds_instance",
		ResourceID:   "orders-db",
		ARN:          "arn:aws:rds:us-east-1:123456789012:db:orders-db",
	},
	{
		Name:         "orders-queue",
		ResourceType: "aws_sqs_queue",
		ResourceID:   "orders-queue",
		ARN:          "arn:aws:sqs:us-east-1:123456789012:orders-queue",
	},
	{
		Name:         "orders-table",
		ResourceType: "aws_dynamodb_table",
		ResourceID:   "orders-table",
		ARN:          "arn:aws:dynamodb:us-east-1:123456789012:table/orders-table",
	},
}

// observabilityCoverageFamilyPostureFixture is the one ec2_instance_posture
// fact: the orders EC2 instance's posture observation, mirroring the
// orders-ec2 target field for field so the committed node carries the exact
// CloudResourceUID the alarm row binds.
type observabilityCoverageFamilyPostureFixture struct {
	InstanceID string
	ARN        string
	State      string
}

// observabilityCoverageFamilyPosture commits the EC2 node the alarm edge
// resolves against (not posture classification, which belongs to the ec2
// families' Odùs).
var observabilityCoverageFamilyPosture = observabilityCoverageFamilyPostureFixture{
	InstanceID: "i-0abc123def4567890",
	ARN:        "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
	State:      "running",
}

// observabilityCoverageFamilyRelationshipFixture describes one
// aws_relationship fact: either a cloudwatch_alarm_observes_metric
// dimension edge from an object to a target identity, or an
// xray_sampling_rule_matches_service service-name anchor.
type observabilityCoverageFamilyRelationshipFixture struct {
	// SourceARN is the observability object's ARN: the relationship
	// source the classifier joins to the object's ref.
	SourceARN string
	// RelationshipType is the relationship verb.
	RelationshipType string
	// DimensionValue is the single metric-dimension identity the
	// relationship resolves (a target ARN, a bare target id, a ghost
	// identity, or empty for the metric-name-only restraint).
	DimensionValue string
	// ServiceName is the X-Ray service-name anchor (service-name
	// restraint only).
	ServiceName string
}

// observabilityCoverageFamilyRelationships are the eight relationships:
// five exact dimension pairs plus the three restraints. The
// dashboard-to-database pair resolves by bare id rather than ARN, proving
// the bare_id resolution mode the other four prove arn against.
var observabilityCoverageFamilyRelationships = []observabilityCoverageFamilyRelationshipFixture{
	{
		// EDGE: orders-cpu-high -> orders EC2 instance by ARN.
		SourceARN:        "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
	},
	{
		// EDGE: checkout-rollup -> orders Lambda by ARN.
		SourceARN:        "arn:aws:cloudwatch:us-east-1:123456789012:alarm:checkout-rollup",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "arn:aws:lambda:us-east-1:123456789012:function:orders-api",
	},
	{
		// EDGE: orders-overview -> orders database by bare id.
		SourceARN:        "arn:aws:cloudwatch:us-east-1:123456789012:dashboard/orders-overview",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "orders-db",
	},
	{
		// EDGE: /orders/api -> orders queue by ARN.
		SourceARN:        "arn:aws:logs:us-east-1:123456789012:log-group:/orders/api",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "arn:aws:sqs:us-east-1:123456789012:orders-queue",
	},
	{
		// EDGE: orders sampling rule -> orders table by ARN.
		SourceARN:        "arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "arn:aws:dynamodb:us-east-1:123456789012:table/orders-table",
	},
	{
		// NO EDGE: metric-name-only alarm carries no dimensions.
		SourceARN:        "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-metric-only",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "",
	},
	{
		// NO EDGE: service-name X-Ray match stays derived.
		SourceARN:        "arn:aws:xray:us-east-1:123456789012:sampling-rule/checkout-service-rule",
		RelationshipType: "xray_sampling_rule_matches_service",
		ServiceName:      "checkout-service",
	},
	{
		// NO EDGE: ghost dimension resolves to no scanned resource.
		SourceARN:        "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-ghost-target",
		RelationshipType: "cloudwatch_alarm_observes_metric",
		DimensionValue:   "arn:aws:ec2:us-east-1:123456789012:instance/i-ghost00000000000000",
	},
}

// observabilityCoverageFamilyStableFactKey derives one fact's durable dedup
// key from the collector's own identity inputs, in a readable
// fixture-local format rather than a hex digest, instead of hand-typing a
// string the expected-edge fixture cannot independently check.
func observabilityCoverageFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		observabilityCoverageFamilyAccountID,
		factKind, identity,
	)
}

// ObservabilityCoverageFamilyOdu builds the cataloged Odù for the
// observability_coverage direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage claim
// built on it is already void.
func ObservabilityCoverageFamilyOdu() CatalogOdu {
	factsForOdu := make(
		[]facts.Envelope, 0,
		len(observabilityCoverageFamilyObjects)+
			len(observabilityCoverageFamilyTargets)+
			len(observabilityCoverageFamilyRelationships)+1,
	)
	for _, fixture := range observabilityCoverageFamilyObjects {
		arn := fixture.ARN
		name := fixture.Name
		resource := awsv1.Resource{
			AccountID:    observabilityCoverageFamilyAccountID,
			ResourceID:   arn,
			Region:       observabilityCoverageFamilyRegion,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
			Name:         &name,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				ObservabilityCoverageFamilyOduName, fixture.Name, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          observabilityCoverageFamilyScopeID,
			GenerationID:     observabilityCoverageFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    observabilityCoverageFamilyStableFactKey(cloud.AWSResourceFactKind, fixture.Name),
			SchemaVersion:    cloud.AWSResourceSchemaVersion,
			CollectorKind:    observabilityCoverageFamilyCollectorKind,
			SourceConfidence: observabilityCoverageFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range observabilityCoverageFamilyTargets {
		resourceID := fixture.ResourceID
		arn := fixture.ARN
		name := fixture.Name
		resource := awsv1.Resource{
			AccountID:    observabilityCoverageFamilyAccountID,
			ResourceID:   resourceID,
			Region:       observabilityCoverageFamilyRegion,
			ResourceType: fixture.ResourceType,
			ARN:          &arn,
			Name:         &name,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				ObservabilityCoverageFamilyOduName, fixture.Name, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          observabilityCoverageFamilyScopeID,
			GenerationID:     observabilityCoverageFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    observabilityCoverageFamilyStableFactKey(cloud.AWSResourceFactKind, fixture.Name),
			SchemaVersion:    cloud.AWSResourceSchemaVersion,
			CollectorKind:    observabilityCoverageFamilyCollectorKind,
			SourceConfidence: observabilityCoverageFamilySourceConfidence,
			Payload:          payload,
		})
	}
	instanceID := observabilityCoverageFamilyPosture.InstanceID
	postureARN := observabilityCoverageFamilyPosture.ARN
	postureState := observabilityCoverageFamilyPosture.State
	postureResourceType := "aws_ec2_instance"
	posture := awsv1.EC2InstancePosture{
		AccountID:    observabilityCoverageFamilyAccountID,
		Region:       observabilityCoverageFamilyRegion,
		InstanceID:   &instanceID,
		ARN:          &postureARN,
		ResourceType: &postureResourceType,
		State:        &postureState,
	}
	posturePayload, err := factschema.EncodeEC2InstancePosture(posture)
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode ec2_instance_posture payload for %q: %v",
			ObservabilityCoverageFamilyOduName, instanceID, err,
		))
	}
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          observabilityCoverageFamilyScopeID,
		GenerationID:     observabilityCoverageFamilyGenerationID,
		FactKind:         cloud.EC2InstancePostureFactKind,
		StableFactKey:    observabilityCoverageFamilyStableFactKey(cloud.EC2InstancePostureFactKind, "orders-ec2"),
		SchemaVersion:    cloud.EC2InstancePostureSchemaVersionV1,
		CollectorKind:    observabilityCoverageFamilyCollectorKind,
		SourceConfidence: observabilityCoverageFamilySourceConfidence,
		Payload:          posturePayload,
	})
	for _, fixture := range observabilityCoverageFamilyRelationships {
		relationship := awsv1.Relationship{
			AccountID:        observabilityCoverageFamilyAccountID,
			Region:           observabilityCoverageFamilyRegion,
			RelationshipType: fixture.RelationshipType,
			SourceResourceID: fixture.SourceARN,
			TargetResourceID: fixture.DimensionValue,
		}
		sourceARN := fixture.SourceARN
		relationship.SourceARN = &sourceARN
		switch fixture.RelationshipType {
		case "cloudwatch_alarm_observes_metric":
			if fixture.DimensionValue == "" {
				break
			}
			relationship.Attributes = map[string]any{
				"attributes": map[string]any{
					"namespace":   "AWS/EC2",
					"metric_name": "CPUUtilization",
					"dimensions": []map[string]any{
						{"name": "InstanceId", "value": fixture.DimensionValue},
					},
				},
			}
		case "xray_sampling_rule_matches_service":
			relationship.Attributes = map[string]any{
				"attributes": map[string]any{
					"service_name": fixture.ServiceName,
				},
			}
		}
		payload, err := factschema.EncodeAWSRelationship(relationship)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_relationship payload for %q: %v",
				ObservabilityCoverageFamilyOduName, fixture.SourceARN, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          observabilityCoverageFamilyScopeID,
			GenerationID:     observabilityCoverageFamilyGenerationID,
			FactKind:         cloud.AWSRelationshipFactKind,
			StableFactKey:    observabilityCoverageFamilyStableFactKey(cloud.AWSRelationshipFactKind, fixture.SourceARN),
			SchemaVersion:    cloud.AWSRelationshipSchemaVersion,
			CollectorKind:    observabilityCoverageFamilyCollectorKind,
			SourceConfidence: observabilityCoverageFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: ObservabilityCoverageFamilyOduName, Facts: factsForOdu},
		Detail: "twenty-two facts for the direct-materialization observability_coverage family: eight aws_resource observability objects (five edge producers, one per coverage signal, plus a metric-name-only alarm, a service-name X-Ray rule, and an alarm naming an unscanned target), five scanned monitored targets, one ec2_instance_posture fact committing the EC2 node the alarm edge resolves against, and eight aws_relationship facts (five exact dimension pairs, one dimensionless pair, one service-name match, and one ghost-dimension pair), so the AWS_COVERS_* expected set proves all five closed-vocabulary signals with arn and bare_id resolution modes and the classifier's no-fabrication restraint",
	}
}
