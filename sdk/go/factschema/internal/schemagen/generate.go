// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// nullType is the JSON Schema type token an optional field accepts in addition
// to its declared type, so an explicit JSON null (the collectors emit null for
// an absent optional via boolOrNil / int32OrNil / a nil pointer) validates.
const nullType = "null"

// schemaBaseID is the $id prefix every generated schema shares. Each kind
// appends its own family/version/name path so the $id is stable and unique.
const schemaBaseID = "https://eshu.dev/schemas/factschema/"

// openAndNullableOptionals post-processes a marshaled JSON Schema so it matches
// what the collectors actually emit, not the narrower reducer-consumed subset the
// typed struct models. It walks every object node and:
//
//   - sets "additionalProperties": true, because every collector payload carries
//     extra context keys the reducer does not consume (collector_instance_id,
//     service_kind, and service-specific fields) plus the nested "attributes"
//     object; the typed struct is the subset the reducer reads, so the schema
//     must permit the richer real payload, consistent with the decode contract
//     (the decoder ignores unmodeled keys).
//   - makes every property NOT listed in that object's "required" array nullable
//     (adds "null" to its type), because the collectors emit an explicit JSON
//     null for an absent optional (boolOrNil / int32OrNil / a nil pointer), and a
//     bare {"type":"string"} would reject null.
//
// It operates on the generic decoded map so it is independent of the reflector's
// pointer/omitempty handling and applies uniformly to nested objects (the
// block-device sub-objects, any nested object schema). Round-tripping through a
// map keeps output deterministic: encoding/json sorts map keys.
func openAndNullableOptionals(node any) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}

	// Collect this object's required set so optional properties can be made
	// nullable. A missing or malformed "required" means every property is
	// optional.
	required := map[string]struct{}{}
	if rawRequired, present := obj["required"].([]any); present {
		for _, r := range rawRequired {
			if name, isString := r.(string); isString {
				required[name] = struct{}{}
			}
		}
	}

	if props, present := obj["properties"].(map[string]any); present {
		obj["additionalProperties"] = true
		for name, rawProp := range props {
			prop, isObj := rawProp.(map[string]any)
			if !isObj {
				continue
			}
			if _, isRequired := required[name]; !isRequired {
				makeTypeNullable(prop)
			}
			// Recurse so nested object schemas (and array item schemas) are
			// opened and their optionals made nullable too.
			openAndNullableOptionals(prop)
		}
	}

	// Recurse into array item schemas so a []struct field's element object is
	// opened and its optionals made nullable.
	if items, present := obj["items"].(map[string]any); present {
		openAndNullableOptionals(items)
	}
}

// makeTypeNullable adds the null type to a property schema's "type" so an
// explicit JSON null validates. It handles both the scalar ({"type":"string"})
// and the already-union ({"type":["string","null"]}) forms and is idempotent.
// A property with no "type" (for example an untyped open object) is left alone —
// it already accepts null.
func makeTypeNullable(prop map[string]any) {
	switch t := prop["type"].(type) {
	case string:
		if t == nullType {
			return
		}
		prop["type"] = []any{t, nullType}
	case []any:
		for _, existing := range t {
			if existing == nullType {
				return
			}
		}
		prop["type"] = append(t, nullType)
	}
}

// reflectSchema returns the canonical, deterministically ordered JSON Schema
// bytes for a typed payload struct, given its $id and human title. Every
// per-kind generator delegates here so all schemas are built identically: the
// reflector runs with DoNotReference so the flat struct inlines directly instead
// of producing a $defs/$ref indirection, and with the default
// RequiredFromJSONSchemaTags=false so "required" is derived from the json tags
// alone (Contract System v1 §3.1): a field is required in the generated schema
// exactly when its json tag carries no `omitempty` option. The decode seam
// derives the same required set from the same struct tags via fields.go's
// payloadKeySetOf, so the generated schema and the runtime validator share one
// source of truth rather than two hand-kept lists. TestDerivedKeySetsMatch-
// GeneratedSchemas locks the two derivations together, and the flat-struct
// convention (TestPayloadStructShapeConvention) keeps "no omitempty ⇒ required"
// equivalent to the "pointer/slice/map ⇒ optional" intuition the docs describe.
//
// allowAdditional controls the top-level "additionalProperties" keyword. Fully
// typed kinds pass false so the schema rejects unknown keys (a renamed or extra
// field is a visible schema-diff break). The aws_resource kind passes true
// because it carries an intentional untyped pass-through (awsv1.Resource's
// Attributes bag) for service-specific fields; a closed schema there would
// falsely reject every valid service attribute.
func reflectSchema(id, title string, v any) ([]byte, error) {
	reflector := &jsonschema.Reflector{
		DoNotReference:            true,
		AllowAdditionalProperties: true,
	}

	schema := reflector.Reflect(v)
	schema.ID = jsonschema.ID(id)
	schema.Title = title

	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schemagen: marshal %s schema: %w", title, err)
	}

	// Post-process so the schema matches the collector-emitted payload, not just
	// the reducer-consumed typed subset: open every object to the extra context
	// and service keys the collectors carry, and make every optional field accept
	// the explicit JSON null the collectors emit for an absent optional.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("schemagen: unmarshal %s schema for post-processing: %w", title, err)
	}
	openAndNullableOptionals(generic)

	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schemagen: marshal post-processed %s schema: %w", title, err)
	}

	return append(out, '\n'), nil
}

// AWSResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_resource" payload.
const AWSResourceSchemaID = schemaBaseID + "aws/v1/resource.schema.json"

// AWSResourceSchema returns the JSON Schema bytes for awsv1.Resource. Both the
// generator's go:generate target and schema_gen_test.go's drift check call this
// function, so a generated artifact and its drift test can never disagree about
// how the schema is built.
func AWSResourceSchema() ([]byte, error) {
	return reflectSchema(AWSResourceSchemaID, "Eshu aws_resource Payload (schema version 1)", &awsv1.Resource{})
}

// AWSRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_relationship" payload.
const AWSRelationshipSchemaID = schemaBaseID + "aws/v1/relationship.schema.json"

// AWSRelationshipSchema returns the JSON Schema bytes for awsv1.Relationship.
func AWSRelationshipSchema() ([]byte, error) {
	return reflectSchema(AWSRelationshipSchemaID, "Eshu aws_relationship Payload (schema version 1)", &awsv1.Relationship{})
}

// AWSSecurityGroupRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_security_group_rule" payload.
const AWSSecurityGroupRuleSchemaID = schemaBaseID + "aws/v1/security_group_rule.schema.json"

// AWSSecurityGroupRuleSchema returns the JSON Schema bytes for
// awsv1.SecurityGroupRule.
func AWSSecurityGroupRuleSchema() ([]byte, error) {
	return reflectSchema(AWSSecurityGroupRuleSchemaID, "Eshu aws_security_group_rule Payload (schema version 1)", &awsv1.SecurityGroupRule{})
}

// EC2InstancePostureSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ec2_instance_posture" payload.
const EC2InstancePostureSchemaID = schemaBaseID + "aws/v1/ec2_instance_posture.schema.json"

// EC2InstancePostureSchema returns the JSON Schema bytes for
// awsv1.EC2InstancePosture.
func EC2InstancePostureSchema() ([]byte, error) {
	return reflectSchema(EC2InstancePostureSchemaID, "Eshu ec2_instance_posture Payload (schema version 1)", &awsv1.EC2InstancePosture{})
}

// S3BucketPostureSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "s3_bucket_posture" payload.
const S3BucketPostureSchemaID = schemaBaseID + "aws/v1/s3_bucket_posture.schema.json"

// S3BucketPostureSchema returns the JSON Schema bytes for awsv1.S3BucketPosture.
func S3BucketPostureSchema() ([]byte, error) {
	return reflectSchema(S3BucketPostureSchemaID, "Eshu s3_bucket_posture Payload (schema version 1)", &awsv1.S3BucketPosture{})
}

// AWSIAMPermissionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_permission" payload.
const AWSIAMPermissionSchemaID = schemaBaseID + "iam/v1/permission.schema.json"

// AWSIAMPermissionSchema returns the JSON Schema bytes for iamv1.Permission.
func AWSIAMPermissionSchema() ([]byte, error) {
	return reflectSchema(AWSIAMPermissionSchemaID, "Eshu aws_iam_permission Payload (schema version 1)", &iamv1.Permission{})
}

// AWSResourcePolicyPermissionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_resource_policy_permission" payload.
const AWSResourcePolicyPermissionSchemaID = schemaBaseID + "iam/v1/resource_policy_permission.schema.json"

// AWSResourcePolicyPermissionSchema returns the JSON Schema bytes for
// iamv1.ResourcePolicyPermission.
func AWSResourcePolicyPermissionSchema() ([]byte, error) {
	return reflectSchema(AWSResourcePolicyPermissionSchemaID, "Eshu aws_resource_policy_permission Payload (schema version 1)", &iamv1.ResourcePolicyPermission{})
}

// AWSIAMPrincipalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_principal" payload.
const AWSIAMPrincipalSchemaID = schemaBaseID + "iam/v1/principal.schema.json"

// AWSIAMPrincipalSchema returns the JSON Schema bytes for iamv1.Principal.
func AWSIAMPrincipalSchema() ([]byte, error) {
	return reflectSchema(AWSIAMPrincipalSchemaID, "Eshu aws_iam_principal Payload (schema version 1)", &iamv1.Principal{})
}

// IncidentRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "incident.record" payload.
const IncidentRecordSchemaID = schemaBaseID + "incident/v1/record.schema.json"

// IncidentRecordSchema returns the JSON Schema bytes for
// incidentv1.IncidentRecord.
func IncidentRecordSchema() ([]byte, error) {
	return reflectSchema(IncidentRecordSchemaID, "Eshu incident.record Payload (schema version 1)", &incidentv1.IncidentRecord{})
}

// IncidentLifecycleEventSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "incident.lifecycle_event" payload.
const IncidentLifecycleEventSchemaID = schemaBaseID + "incident/v1/lifecycle_event.schema.json"

// IncidentLifecycleEventSchema returns the JSON Schema bytes for
// incidentv1.LifecycleEvent.
func IncidentLifecycleEventSchema() ([]byte, error) {
	return reflectSchema(IncidentLifecycleEventSchemaID, "Eshu incident.lifecycle_event Payload (schema version 1)", &incidentv1.LifecycleEvent{})
}

// ChangeRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "change.record" payload.
const ChangeRecordSchemaID = schemaBaseID + "incident/v1/change_record.schema.json"

// ChangeRecordSchema returns the JSON Schema bytes for incidentv1.ChangeRecord.
func ChangeRecordSchema() ([]byte, error) {
	return reflectSchema(ChangeRecordSchemaID, "Eshu change.record Payload (schema version 1)", &incidentv1.ChangeRecord{})
}

// IncidentRoutingAppliedPagerDutyResourceSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "incident_routing.applied_pagerduty_resource"
// payload.
const IncidentRoutingAppliedPagerDutyResourceSchemaID = schemaBaseID + "incident/v1/applied_pagerduty_resource.schema.json"

// IncidentRoutingAppliedPagerDutyResourceSchema returns the JSON Schema bytes
// for incidentv1.AppliedPagerDutyResource.
func IncidentRoutingAppliedPagerDutyResourceSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingAppliedPagerDutyResourceSchemaID, "Eshu incident_routing.applied_pagerduty_resource Payload (schema version 1)", &incidentv1.AppliedPagerDutyResource{})
}

// IncidentRoutingAppliedAlertRouteSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "incident_routing.applied_alert_route" payload.
const IncidentRoutingAppliedAlertRouteSchemaID = schemaBaseID + "incident/v1/applied_alert_route.schema.json"

// IncidentRoutingAppliedAlertRouteSchema returns the JSON Schema bytes for
// incidentv1.AppliedAlertRoute.
func IncidentRoutingAppliedAlertRouteSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingAppliedAlertRouteSchemaID, "Eshu incident_routing.applied_alert_route Payload (schema version 1)", &incidentv1.AppliedAlertRoute{})
}

// IncidentRoutingObservedPagerDutyServiceSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "incident_routing.observed_pagerduty_service"
// payload.
const IncidentRoutingObservedPagerDutyServiceSchemaID = schemaBaseID + "incident/v1/observed_pagerduty_service.schema.json"

// IncidentRoutingObservedPagerDutyServiceSchema returns the JSON Schema bytes
// for incidentv1.ObservedPagerDutyService.
func IncidentRoutingObservedPagerDutyServiceSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingObservedPagerDutyServiceSchemaID, "Eshu incident_routing.observed_pagerduty_service Payload (schema version 1)", &incidentv1.ObservedPagerDutyService{})
}

// IncidentRoutingObservedPagerDutyIntegrationSchemaID is the checked-in JSON
// Schema $id for the schema-version-1
// "incident_routing.observed_pagerduty_integration" payload.
const IncidentRoutingObservedPagerDutyIntegrationSchemaID = schemaBaseID + "incident/v1/observed_pagerduty_integration.schema.json"

// IncidentRoutingObservedPagerDutyIntegrationSchema returns the JSON Schema
// bytes for incidentv1.ObservedPagerDutyIntegration.
func IncidentRoutingObservedPagerDutyIntegrationSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingObservedPagerDutyIntegrationSchemaID, "Eshu incident_routing.observed_pagerduty_integration Payload (schema version 1)", &incidentv1.ObservedPagerDutyIntegration{})
}

// IncidentRoutingCoverageWarningSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "incident_routing.coverage_warning" payload.
const IncidentRoutingCoverageWarningSchemaID = schemaBaseID + "incident/v1/coverage_warning.schema.json"

// IncidentRoutingCoverageWarningSchema returns the JSON Schema bytes for
// incidentv1.CoverageWarning.
func IncidentRoutingCoverageWarningSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingCoverageWarningSchemaID, "Eshu incident_routing.coverage_warning Payload (schema version 1)", &incidentv1.CoverageWarning{})
}
