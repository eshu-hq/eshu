// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen //nolint:filelength // per-family schema-generator registry; one SchemaID const + Schema func pair per migrated fact kind, reviewed as a single generator table. Splitting per-family is a separate refactor.

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
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

// GCPCloudResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1.1.0 "gcp_cloud_resource" payload.
const GCPCloudResourceSchemaID = schemaBaseID + "gcp/v1/resource.schema.json"

// GCPCloudResourceSchema returns the JSON Schema bytes for gcpv1.Resource.
// The title names schema version 1.1.0 (facts.GCPCloudResourceSchemaVersion)
// because this kind is one minor ahead of the rest of the gcp family; the
// decode seam still dispatches on the schema-version major only.
func GCPCloudResourceSchema() ([]byte, error) {
	return reflectSchema(GCPCloudResourceSchemaID, "Eshu gcp_cloud_resource Payload (schema version 1.1.0)", &gcpv1.Resource{})
}

// GCPCloudRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_cloud_relationship" payload.
const GCPCloudRelationshipSchemaID = schemaBaseID + "gcp/v1/relationship.schema.json"

// GCPCloudRelationshipSchema returns the JSON Schema bytes for
// gcpv1.Relationship.
func GCPCloudRelationshipSchema() ([]byte, error) {
	return reflectSchema(GCPCloudRelationshipSchemaID, "Eshu gcp_cloud_relationship Payload (schema version 1)", &gcpv1.Relationship{})
}

// GCPCollectionWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_collection_warning" payload.
const GCPCollectionWarningSchemaID = schemaBaseID + "gcp/v1/collection_warning.schema.json"

// GCPCollectionWarningSchema returns the JSON Schema bytes for
// gcpv1.CollectionWarning.
func GCPCollectionWarningSchema() ([]byte, error) {
	return reflectSchema(GCPCollectionWarningSchemaID, "Eshu gcp_collection_warning Payload (schema version 1)", &gcpv1.CollectionWarning{})
}

// GCPDNSRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_dns_record" payload.
const GCPDNSRecordSchemaID = schemaBaseID + "gcp/v1/dns_record.schema.json"

// GCPDNSRecordSchema returns the JSON Schema bytes for gcpv1.DNSRecord.
func GCPDNSRecordSchema() ([]byte, error) {
	return reflectSchema(GCPDNSRecordSchemaID, "Eshu gcp_dns_record Payload (schema version 1)", &gcpv1.DNSRecord{})
}

// GCPIAMPolicyObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_iam_policy_observation" payload.
const GCPIAMPolicyObservationSchemaID = schemaBaseID + "gcp/v1/iam_policy_observation.schema.json"

// GCPIAMPolicyObservationSchema returns the JSON Schema bytes for
// gcpv1.IAMPolicyObservation.
func GCPIAMPolicyObservationSchema() ([]byte, error) {
	return reflectSchema(GCPIAMPolicyObservationSchemaID, "Eshu gcp_iam_policy_observation Payload (schema version 1)", &gcpv1.IAMPolicyObservation{})
}

// AzureCloudResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_cloud_resource" payload.
const AzureCloudResourceSchemaID = schemaBaseID + "azure/v1/cloud_resource.schema.json"

// AzureCloudResourceSchema returns the JSON Schema bytes for
// azurev1.CloudResource.
func AzureCloudResourceSchema() ([]byte, error) {
	return reflectSchema(AzureCloudResourceSchemaID, "Eshu azure_cloud_resource Payload (schema version 1)", &azurev1.CloudResource{})
}

// AzureCloudRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_cloud_relationship" payload.
const AzureCloudRelationshipSchemaID = schemaBaseID + "azure/v1/cloud_relationship.schema.json"

// AzureCloudRelationshipSchema returns the JSON Schema bytes for
// azurev1.CloudRelationship.
func AzureCloudRelationshipSchema() ([]byte, error) {
	return reflectSchema(AzureCloudRelationshipSchemaID, "Eshu azure_cloud_relationship Payload (schema version 1)", &azurev1.CloudRelationship{})
}

// AzureDNSRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_dns_record" payload.
const AzureDNSRecordSchemaID = schemaBaseID + "azure/v1/dns_record.schema.json"

// AzureDNSRecordSchema returns the JSON Schema bytes for azurev1.DNSRecord.
func AzureDNSRecordSchema() ([]byte, error) {
	return reflectSchema(AzureDNSRecordSchemaID, "Eshu azure_dns_record Payload (schema version 1)", &azurev1.DNSRecord{})
}

// AzureCollectionWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_collection_warning" payload.
const AzureCollectionWarningSchemaID = schemaBaseID + "azure/v1/collection_warning.schema.json"

// AzureCollectionWarningSchema returns the JSON Schema bytes for
// azurev1.CollectionWarning.
func AzureCollectionWarningSchema() ([]byte, error) {
	return reflectSchema(AzureCollectionWarningSchemaID, "Eshu azure_collection_warning Payload (schema version 1)", &azurev1.CollectionWarning{})
}

// KubernetesLivePodTemplateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.pod_template" payload.
const KubernetesLivePodTemplateSchemaID = schemaBaseID + "kuberneteslive/v1/pod_template.schema.json"

// KubernetesLivePodTemplateSchema returns the JSON Schema bytes for
// kuberneteslivev1.PodTemplate.
func KubernetesLivePodTemplateSchema() ([]byte, error) {
	return reflectSchema(KubernetesLivePodTemplateSchemaID, "Eshu kubernetes_live.pod_template Payload (schema version 1)", &kuberneteslivev1.PodTemplate{})
}

// KubernetesLiveRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.relationship" payload.
const KubernetesLiveRelationshipSchemaID = schemaBaseID + "kuberneteslive/v1/relationship.schema.json"

// KubernetesLiveRelationshipSchema returns the JSON Schema bytes for
// kuberneteslivev1.Relationship.
func KubernetesLiveRelationshipSchema() ([]byte, error) {
	return reflectSchema(KubernetesLiveRelationshipSchemaID, "Eshu kubernetes_live.relationship Payload (schema version 1)", &kuberneteslivev1.Relationship{})
}

// KubernetesLiveWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.warning" payload.
const KubernetesLiveWarningSchemaID = schemaBaseID + "kuberneteslive/v1/warning.schema.json"

// KubernetesLiveWarningSchema returns the JSON Schema bytes for
// kuberneteslivev1.Warning.
func KubernetesLiveWarningSchema() ([]byte, error) {
	return reflectSchema(KubernetesLiveWarningSchemaID, "Eshu kubernetes_live.warning Payload (schema version 1)", &kuberneteslivev1.Warning{})
}

// OCIRegistryRepositorySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.repository" payload.
const OCIRegistryRepositorySchemaID = schemaBaseID + "ociregistry/v1/repository.schema.json"

// OCIRegistryRepositorySchema returns the JSON Schema bytes for
// ociregistryv1.Repository.
func OCIRegistryRepositorySchema() ([]byte, error) {
	return reflectSchema(OCIRegistryRepositorySchemaID, "Eshu oci_registry.repository Payload (schema version 1)", &ociregistryv1.Repository{})
}

// OCIImageManifestSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_manifest" payload.
const OCIImageManifestSchemaID = schemaBaseID + "ociregistry/v1/image_manifest.schema.json"

// OCIImageManifestSchema returns the JSON Schema bytes for
// ociregistryv1.ImageManifest.
func OCIImageManifestSchema() ([]byte, error) {
	return reflectSchema(OCIImageManifestSchemaID, "Eshu oci_registry.image_manifest Payload (schema version 1)", &ociregistryv1.ImageManifest{})
}

// OCIImageIndexSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_index" payload.
const OCIImageIndexSchemaID = schemaBaseID + "ociregistry/v1/image_index.schema.json"

// OCIImageIndexSchema returns the JSON Schema bytes for
// ociregistryv1.ImageIndex.
func OCIImageIndexSchema() ([]byte, error) {
	return reflectSchema(OCIImageIndexSchemaID, "Eshu oci_registry.image_index Payload (schema version 1)", &ociregistryv1.ImageIndex{})
}

// OCIImageDescriptorSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_descriptor" payload.
const OCIImageDescriptorSchemaID = schemaBaseID + "ociregistry/v1/image_descriptor.schema.json"

// OCIImageDescriptorSchema returns the JSON Schema bytes for
// ociregistryv1.ImageDescriptor.
func OCIImageDescriptorSchema() ([]byte, error) {
	return reflectSchema(OCIImageDescriptorSchemaID, "Eshu oci_registry.image_descriptor Payload (schema version 1)", &ociregistryv1.ImageDescriptor{})
}

// OCIImageTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_tag_observation" payload.
const OCIImageTagObservationSchemaID = schemaBaseID + "ociregistry/v1/tag_observation.schema.json"

// OCIImageTagObservationSchema returns the JSON Schema bytes for
// ociregistryv1.TagObservation.
func OCIImageTagObservationSchema() ([]byte, error) {
	return reflectSchema(OCIImageTagObservationSchemaID, "Eshu oci_registry.image_tag_observation Payload (schema version 1)", &ociregistryv1.TagObservation{})
}

// OCIImageReferrerSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_referrer" payload.
const OCIImageReferrerSchemaID = schemaBaseID + "ociregistry/v1/image_referrer.schema.json"

// OCIImageReferrerSchema returns the JSON Schema bytes for
// ociregistryv1.ImageReferrer.
func OCIImageReferrerSchema() ([]byte, error) {
	return reflectSchema(OCIImageReferrerSchemaID, "Eshu oci_registry.image_referrer Payload (schema version 1)", &ociregistryv1.ImageReferrer{})
}

// OCIRegistryWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.warning" payload.
const OCIRegistryWarningSchemaID = schemaBaseID + "ociregistry/v1/warning.schema.json"

// OCIRegistryWarningSchema returns the JSON Schema bytes for
// ociregistryv1.Warning. This kind is deferred (typed-but-not-consumed), but
// its schema is still generated so the kind is contract-complete.
func OCIRegistryWarningSchema() ([]byte, error) {
	return reflectSchema(OCIRegistryWarningSchemaID, "Eshu oci_registry.warning Payload (schema version 1)", &ociregistryv1.Warning{})
}

// TerraformStateSnapshotSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_snapshot" payload.
const TerraformStateSnapshotSchemaID = schemaBaseID + "terraformstate/v1/snapshot.schema.json"

// TerraformStateSnapshotSchema returns the JSON Schema bytes for
// tfstatev1.Snapshot.
func TerraformStateSnapshotSchema() ([]byte, error) {
	return reflectSchema(TerraformStateSnapshotSchemaID, "Eshu terraform_state_snapshot Payload (schema version 1)", &tfstatev1.Snapshot{})
}

// TerraformStateResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_resource" payload.
const TerraformStateResourceSchemaID = schemaBaseID + "terraformstate/v1/resource.schema.json"

// TerraformStateResourceSchema returns the JSON Schema bytes for
// tfstatev1.Resource.
func TerraformStateResourceSchema() ([]byte, error) {
	return reflectSchema(TerraformStateResourceSchemaID, "Eshu terraform_state_resource Payload (schema version 1)", &tfstatev1.Resource{})
}

// TerraformStateModuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_module" payload.
const TerraformStateModuleSchemaID = schemaBaseID + "terraformstate/v1/module.schema.json"

// TerraformStateModuleSchema returns the JSON Schema bytes for
// tfstatev1.Module.
func TerraformStateModuleSchema() ([]byte, error) {
	return reflectSchema(TerraformStateModuleSchemaID, "Eshu terraform_state_module Payload (schema version 1)", &tfstatev1.Module{})
}

// TerraformStateOutputSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_output" payload.
const TerraformStateOutputSchemaID = schemaBaseID + "terraformstate/v1/output.schema.json"

// TerraformStateOutputSchema returns the JSON Schema bytes for
// tfstatev1.Output.
func TerraformStateOutputSchema() ([]byte, error) {
	return reflectSchema(TerraformStateOutputSchemaID, "Eshu terraform_state_output Payload (schema version 1)", &tfstatev1.Output{})
}

// TerraformStateTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_tag_observation" payload.
const TerraformStateTagObservationSchemaID = schemaBaseID + "terraformstate/v1/tag_observation.schema.json"

// TerraformStateTagObservationSchema returns the JSON Schema bytes for
// tfstatev1.TagObservation.
func TerraformStateTagObservationSchema() ([]byte, error) {
	return reflectSchema(TerraformStateTagObservationSchemaID, "Eshu terraform_state_tag_observation Payload (schema version 1)", &tfstatev1.TagObservation{})
}

// TerraformStateCandidateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_candidate" payload.
const TerraformStateCandidateSchemaID = schemaBaseID + "terraformstate/v1/candidate.schema.json"

// TerraformStateCandidateSchema returns the JSON Schema bytes for
// tfstatev1.Candidate.
func TerraformStateCandidateSchema() ([]byte, error) {
	return reflectSchema(TerraformStateCandidateSchemaID, "Eshu terraform_state_candidate Payload (schema version 1)", &tfstatev1.Candidate{})
}

// TerraformStateProviderBindingSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "terraform_state_provider_binding" payload.
const TerraformStateProviderBindingSchemaID = schemaBaseID + "terraformstate/v1/provider_binding.schema.json"

// TerraformStateProviderBindingSchema returns the JSON Schema bytes for
// tfstatev1.ProviderBinding.
func TerraformStateProviderBindingSchema() ([]byte, error) {
	return reflectSchema(TerraformStateProviderBindingSchemaID, "Eshu terraform_state_provider_binding Payload (schema version 1)", &tfstatev1.ProviderBinding{})
}

// TerraformStateWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "terraform_state_warning" payload.
const TerraformStateWarningSchemaID = schemaBaseID + "terraformstate/v1/warning.schema.json"

// TerraformStateWarningSchema returns the JSON Schema bytes for
// tfstatev1.Warning.
func TerraformStateWarningSchema() ([]byte, error) {
	return reflectSchema(TerraformStateWarningSchemaID, "Eshu terraform_state_warning Payload (schema version 1)", &tfstatev1.Warning{})
}

// PackageRegistryPackageSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.package" payload.
const PackageRegistryPackageSchemaID = schemaBaseID + "packageregistry/v1/package.schema.json"

// PackageRegistryPackageSchema returns the JSON Schema bytes for
// packageregistryv1.Package.
func PackageRegistryPackageSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageSchemaID, "Eshu package_registry.package Payload (schema version 1)", &packageregistryv1.Package{})
}

// PackageRegistryPackageVersionSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.package_version" payload.
const PackageRegistryPackageVersionSchemaID = schemaBaseID + "packageregistry/v1/package_version.schema.json"

// PackageRegistryPackageVersionSchema returns the JSON Schema bytes for
// packageregistryv1.PackageVersion.
func PackageRegistryPackageVersionSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageVersionSchemaID, "Eshu package_registry.package_version Payload (schema version 1)", &packageregistryv1.PackageVersion{})
}

// PackageRegistryPackageDependencySchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.package_dependency" payload.
const PackageRegistryPackageDependencySchemaID = schemaBaseID + "packageregistry/v1/package_dependency.schema.json"

// PackageRegistryPackageDependencySchema returns the JSON Schema bytes for
// packageregistryv1.PackageDependency.
func PackageRegistryPackageDependencySchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageDependencySchemaID, "Eshu package_registry.package_dependency Payload (schema version 1)", &packageregistryv1.PackageDependency{})
}

// PackageRegistrySourceHintSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.source_hint" payload.
const PackageRegistrySourceHintSchemaID = schemaBaseID + "packageregistry/v1/source_hint.schema.json"

// PackageRegistrySourceHintSchema returns the JSON Schema bytes for
// packageregistryv1.SourceHint.
func PackageRegistrySourceHintSchema() ([]byte, error) {
	return reflectSchema(PackageRegistrySourceHintSchemaID, "Eshu package_registry.source_hint Payload (schema version 1)", &packageregistryv1.SourceHint{})
}

// PackageRegistryPackageArtifactSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.package_artifact" payload.
const PackageRegistryPackageArtifactSchemaID = schemaBaseID + "packageregistry/v1/package_artifact.schema.json"

// PackageRegistryPackageArtifactSchema returns the JSON Schema bytes for
// packageregistryv1.PackageArtifact.
func PackageRegistryPackageArtifactSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageArtifactSchemaID, "Eshu package_registry.package_artifact Payload (schema version 1)", &packageregistryv1.PackageArtifact{})
}

// PackageRegistryVulnerabilityHintSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.vulnerability_hint" payload.
const PackageRegistryVulnerabilityHintSchemaID = schemaBaseID + "packageregistry/v1/vulnerability_hint.schema.json"

// PackageRegistryVulnerabilityHintSchema returns the JSON Schema bytes for
// packageregistryv1.VulnerabilityHint.
func PackageRegistryVulnerabilityHintSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryVulnerabilityHintSchemaID, "Eshu package_registry.vulnerability_hint Payload (schema version 1)", &packageregistryv1.VulnerabilityHint{})
}

// PackageRegistryRegistryEventSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.registry_event" payload.
const PackageRegistryRegistryEventSchemaID = schemaBaseID + "packageregistry/v1/registry_event.schema.json"

// PackageRegistryRegistryEventSchema returns the JSON Schema bytes for
// packageregistryv1.RegistryEvent.
func PackageRegistryRegistryEventSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryRegistryEventSchemaID, "Eshu package_registry.registry_event Payload (schema version 1)", &packageregistryv1.RegistryEvent{})
}

// PackageRegistryRepositoryHostingSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.repository_hosting" payload.
const PackageRegistryRepositoryHostingSchemaID = schemaBaseID + "packageregistry/v1/repository_hosting.schema.json"

// PackageRegistryRepositoryHostingSchema returns the JSON Schema bytes for
// packageregistryv1.RepositoryHosting.
func PackageRegistryRepositoryHostingSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryRepositoryHostingSchemaID, "Eshu package_registry.repository_hosting Payload (schema version 1)", &packageregistryv1.RepositoryHosting{})
}

// PackageRegistryWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.warning" payload.
const PackageRegistryWarningSchemaID = schemaBaseID + "packageregistry/v1/warning.schema.json"

// PackageRegistryWarningSchema returns the JSON Schema bytes for
// packageregistryv1.Warning.
func PackageRegistryWarningSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryWarningSchemaID, "Eshu package_registry.warning Payload (schema version 1)", &packageregistryv1.Warning{})
}

// SBOMDocumentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.document" payload.
const SBOMDocumentSchemaID = schemaBaseID + "sbom/v1/document.schema.json"

// SBOMDocumentSchema returns the JSON Schema bytes for sbomv1.Document.
func SBOMDocumentSchema() ([]byte, error) {
	return reflectSchema(SBOMDocumentSchemaID, "Eshu sbom.document Payload (schema version 1)", &sbomv1.Document{})
}

// SBOMComponentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.component" payload.
const SBOMComponentSchemaID = schemaBaseID + "sbom/v1/component.schema.json"

// SBOMComponentSchema returns the JSON Schema bytes for sbomv1.Component.
func SBOMComponentSchema() ([]byte, error) {
	return reflectSchema(SBOMComponentSchemaID, "Eshu sbom.component Payload (schema version 1)", &sbomv1.Component{})
}

// SBOMDependencyRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.dependency_relationship" payload.
const SBOMDependencyRelationshipSchemaID = schemaBaseID + "sbom/v1/dependency_relationship.schema.json"

// SBOMDependencyRelationshipSchema returns the JSON Schema bytes for
// sbomv1.DependencyRelationship.
func SBOMDependencyRelationshipSchema() ([]byte, error) {
	return reflectSchema(SBOMDependencyRelationshipSchemaID, "Eshu sbom.dependency_relationship Payload (schema version 1)", &sbomv1.DependencyRelationship{})
}

// SBOMExternalReferenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.external_reference" payload.
const SBOMExternalReferenceSchemaID = schemaBaseID + "sbom/v1/external_reference.schema.json"

// SBOMExternalReferenceSchema returns the JSON Schema bytes for
// sbomv1.ExternalReference.
func SBOMExternalReferenceSchema() ([]byte, error) {
	return reflectSchema(SBOMExternalReferenceSchemaID, "Eshu sbom.external_reference Payload (schema version 1)", &sbomv1.ExternalReference{})
}

// SBOMWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.warning" payload.
const SBOMWarningSchemaID = schemaBaseID + "sbom/v1/warning.schema.json"

// SBOMWarningSchema returns the JSON Schema bytes for sbomv1.Warning.
func SBOMWarningSchema() ([]byte, error) {
	return reflectSchema(SBOMWarningSchemaID, "Eshu sbom.warning Payload (schema version 1)", &sbomv1.Warning{})
}

// AttestationStatementSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "attestation.statement" payload.
const AttestationStatementSchemaID = schemaBaseID + "sbom/v1/statement.schema.json"

// AttestationStatementSchema returns the JSON Schema bytes for
// sbomv1.Statement.
func AttestationStatementSchema() ([]byte, error) {
	return reflectSchema(AttestationStatementSchemaID, "Eshu attestation.statement Payload (schema version 1)", &sbomv1.Statement{})
}

// AttestationSignatureVerificationSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "attestation.signature_verification" payload.
const AttestationSignatureVerificationSchemaID = schemaBaseID + "sbom/v1/signature_verification.schema.json"

// AttestationSignatureVerificationSchema returns the JSON Schema bytes for
// sbomv1.SignatureVerification.
func AttestationSignatureVerificationSchema() ([]byte, error) {
	return reflectSchema(AttestationSignatureVerificationSchemaID, "Eshu attestation.signature_verification Payload (schema version 1)", &sbomv1.SignatureVerification{})
}

// AttestationSLSAProvenanceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "attestation.slsa_provenance" payload.
const AttestationSLSAProvenanceSchemaID = schemaBaseID + "sbom/v1/slsa_provenance.schema.json"

// AttestationSLSAProvenanceSchema returns the JSON Schema bytes for
// sbomv1.SLSAProvenance.
func AttestationSLSAProvenanceSchema() ([]byte, error) {
	return reflectSchema(AttestationSLSAProvenanceSchemaID, "Eshu attestation.slsa_provenance Payload (schema version 1)", &sbomv1.SLSAProvenance{})
}

// VulnerabilityCVESchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vulnerability.cve" payload.
const VulnerabilityCVESchemaID = schemaBaseID + "vulnerability/v1/cve.schema.json"

// VulnerabilityCVESchema returns the JSON Schema bytes for
// vulnerabilityv1.CVE.
func VulnerabilityCVESchema() ([]byte, error) {
	return reflectSchema(VulnerabilityCVESchemaID, "Eshu vulnerability.cve Payload (schema version 1)", &vulnerabilityv1.CVE{})
}

// VulnerabilityAffectedPackageSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "vulnerability.affected_package" payload.
const VulnerabilityAffectedPackageSchemaID = schemaBaseID + "vulnerability/v1/affected_package.schema.json"

// VulnerabilityAffectedPackageSchema returns the JSON Schema bytes for
// vulnerabilityv1.AffectedPackage.
func VulnerabilityAffectedPackageSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityAffectedPackageSchemaID, "Eshu vulnerability.affected_package Payload (schema version 1)", &vulnerabilityv1.AffectedPackage{})
}

// VulnerabilityAffectedProductSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "vulnerability.affected_product" payload.
const VulnerabilityAffectedProductSchemaID = schemaBaseID + "vulnerability/v1/affected_product.schema.json"

// VulnerabilityAffectedProductSchema returns the JSON Schema bytes for
// vulnerabilityv1.AffectedProduct.
func VulnerabilityAffectedProductSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityAffectedProductSchemaID, "Eshu vulnerability.affected_product Payload (schema version 1)", &vulnerabilityv1.AffectedProduct{})
}

// VulnerabilityOSPackageSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vulnerability.os_package" payload.
const VulnerabilityOSPackageSchemaID = schemaBaseID + "vulnerability/v1/os_package.schema.json"

// VulnerabilityOSPackageSchema returns the JSON Schema bytes for
// vulnerabilityv1.OSPackage.
func VulnerabilityOSPackageSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityOSPackageSchemaID, "Eshu vulnerability.os_package Payload (schema version 1)", &vulnerabilityv1.OSPackage{})
}

// VulnerabilityEPSSScoreSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vulnerability.epss_score" payload.
const VulnerabilityEPSSScoreSchemaID = schemaBaseID + "vulnerability/v1/epss_score.schema.json"

// VulnerabilityEPSSScoreSchema returns the JSON Schema bytes for
// vulnerabilityv1.EPSSScore.
func VulnerabilityEPSSScoreSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityEPSSScoreSchemaID, "Eshu vulnerability.epss_score Payload (schema version 1)", &vulnerabilityv1.EPSSScore{})
}

// VulnerabilityKnownExploitedSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "vulnerability.known_exploited" payload.
const VulnerabilityKnownExploitedSchemaID = schemaBaseID + "vulnerability/v1/known_exploited.schema.json"

// VulnerabilityKnownExploitedSchema returns the JSON Schema bytes for
// vulnerabilityv1.KnownExploited.
func VulnerabilityKnownExploitedSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityKnownExploitedSchemaID, "Eshu vulnerability.known_exploited Payload (schema version 1)", &vulnerabilityv1.KnownExploited{})
}

// VulnerabilityGoModuleEvidenceSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "vulnerability.go_module_evidence" payload.
const VulnerabilityGoModuleEvidenceSchemaID = schemaBaseID + "vulnerability/v1/go_module_evidence.schema.json"

// VulnerabilityGoModuleEvidenceSchema returns the JSON Schema bytes for
// vulnerabilityv1.GoModuleEvidence.
func VulnerabilityGoModuleEvidenceSchema() ([]byte, error) {
	return reflectSchema(VulnerabilityGoModuleEvidenceSchemaID, "Eshu vulnerability.go_module_evidence Payload (schema version 1)", &vulnerabilityv1.GoModuleEvidence{})
}

// VulnerabilityGoCallReachabilitySchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "vulnerability.go_call_reachability" payload.
const VulnerabilityGoCallReachabilitySchemaID = schemaBaseID + "vulnerability/v1/go_call_reachability.schema.json"

// VulnerabilityGoCallReachabilitySchema returns the JSON Schema bytes for
// vulnerabilityv1.GoCallReachability.
func VulnerabilityGoCallReachabilitySchema() ([]byte, error) {
	return reflectSchema(VulnerabilityGoCallReachabilitySchemaID, "Eshu vulnerability.go_call_reachability Payload (schema version 1)", &vulnerabilityv1.GoCallReachability{})
}

// CodegraphFileSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "file" payload.
const CodegraphFileSchemaID = schemaBaseID + "codegraph/v1/file.schema.json"

// CodegraphFileSchema returns the JSON Schema bytes for codegraphv1.File.
func CodegraphFileSchema() ([]byte, error) {
	return reflectSchema(CodegraphFileSchemaID, "Eshu file Payload (schema version 1)", &codegraphv1.File{})
}

// CodegraphRepositorySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "repository" payload.
const CodegraphRepositorySchemaID = schemaBaseID + "codegraph/v1/repository.schema.json"

// CodegraphRepositorySchema returns the JSON Schema bytes for
// codegraphv1.Repository.
func CodegraphRepositorySchema() ([]byte, error) {
	return reflectSchema(CodegraphRepositorySchemaID, "Eshu repository Payload (schema version 1)", &codegraphv1.Repository{})
}

// CodeDataflowScannedSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_dataflow_scanned" payload.
const CodeDataflowScannedSchemaID = schemaBaseID + "codedataflow/v1/code_dataflow_scanned.schema.json"

// CodeDataflowScannedSchema returns the JSON Schema bytes for
// codedataflowv1.DataflowScanned.
func CodeDataflowScannedSchema() ([]byte, error) {
	return reflectSchema(CodeDataflowScannedSchemaID, "Eshu code_dataflow_scanned Payload (schema version 1)", &codedataflowv1.DataflowScanned{})
}

// CodeDataflowFunctionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_dataflow_function" payload.
const CodeDataflowFunctionSchemaID = schemaBaseID + "codedataflow/v1/code_dataflow_function.schema.json"

// CodeDataflowFunctionSchema returns the JSON Schema bytes for
// codedataflowv1.DataflowFunction.
func CodeDataflowFunctionSchema() ([]byte, error) {
	return reflectSchema(CodeDataflowFunctionSchemaID, "Eshu code_dataflow_function Payload (schema version 1)", &codedataflowv1.DataflowFunction{})
}

// CodeFunctionSummarySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_function_summary" payload.
const CodeFunctionSummarySchemaID = schemaBaseID + "codedataflow/v1/code_function_summary.schema.json"

// CodeFunctionSummarySchema returns the JSON Schema bytes for
// codedataflowv1.FunctionSummary.
func CodeFunctionSummarySchema() ([]byte, error) {
	return reflectSchema(CodeFunctionSummarySchemaID, "Eshu code_function_summary Payload (schema version 1)", &codedataflowv1.FunctionSummary{})
}

// CodeFunctionSourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_function_source" payload.
const CodeFunctionSourceSchemaID = schemaBaseID + "codedataflow/v1/code_function_source.schema.json"

// CodeFunctionSourceSchema returns the JSON Schema bytes for
// codedataflowv1.FunctionSource.
func CodeFunctionSourceSchema() ([]byte, error) {
	return reflectSchema(CodeFunctionSourceSchemaID, "Eshu code_function_source Payload (schema version 1)", &codedataflowv1.FunctionSource{})
}

// CodeTaintEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_taint_evidence" payload.
const CodeTaintEvidenceSchemaID = schemaBaseID + "codedataflow/v1/code_taint_evidence.schema.json"

// CodeTaintEvidenceSchema returns the JSON Schema bytes for
// codedataflowv1.TaintEvidence.
func CodeTaintEvidenceSchema() ([]byte, error) {
	return reflectSchema(CodeTaintEvidenceSchemaID, "Eshu code_taint_evidence Payload (schema version 1)", &codedataflowv1.TaintEvidence{})
}

// CodeInterprocEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "code_interproc_evidence" payload.
const CodeInterprocEvidenceSchemaID = schemaBaseID + "codedataflow/v1/code_interproc_evidence.schema.json"

// CodeInterprocEvidenceSchema returns the JSON Schema bytes for
// codedataflowv1.InterprocEvidence.
func CodeInterprocEvidenceSchema() ([]byte, error) {
	return reflectSchema(CodeInterprocEvidenceSchemaID, "Eshu code_interproc_evidence Payload (schema version 1)", &codedataflowv1.InterprocEvidence{})
}

// CICDRunSchemaID is the checked-in JSON Schema $id for the schema-version-1
// "ci.run" payload.
const CICDRunSchemaID = schemaBaseID + "cicdrun/v1/run.schema.json"

// CICDRunSchema returns the JSON Schema bytes for cicdrunv1.Run.
func CICDRunSchema() ([]byte, error) {
	return reflectSchema(CICDRunSchemaID, "Eshu ci.run Payload (schema version 1)", &cicdrunv1.Run{})
}

// CICDArtifactSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.artifact" payload.
const CICDArtifactSchemaID = schemaBaseID + "cicdrun/v1/artifact.schema.json"

// CICDArtifactSchema returns the JSON Schema bytes for cicdrunv1.Artifact.
func CICDArtifactSchema() ([]byte, error) {
	return reflectSchema(CICDArtifactSchemaID, "Eshu ci.artifact Payload (schema version 1)", &cicdrunv1.Artifact{})
}

// CICDEnvironmentObservationSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "ci.environment_observation" payload.
const CICDEnvironmentObservationSchemaID = schemaBaseID + "cicdrun/v1/environment_observation.schema.json"

// CICDEnvironmentObservationSchema returns the JSON Schema bytes for
// cicdrunv1.EnvironmentObservation.
func CICDEnvironmentObservationSchema() ([]byte, error) {
	return reflectSchema(CICDEnvironmentObservationSchemaID, "Eshu ci.environment_observation Payload (schema version 1)", &cicdrunv1.EnvironmentObservation{})
}

// CICDTriggerEdgeSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.trigger_edge" payload.
const CICDTriggerEdgeSchemaID = schemaBaseID + "cicdrun/v1/trigger_edge.schema.json"

// CICDTriggerEdgeSchema returns the JSON Schema bytes for
// cicdrunv1.TriggerEdge.
func CICDTriggerEdgeSchema() ([]byte, error) {
	return reflectSchema(CICDTriggerEdgeSchemaID, "Eshu ci.trigger_edge Payload (schema version 1)", &cicdrunv1.TriggerEdge{})
}

// CICDStepSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.step" payload.
const CICDStepSchemaID = schemaBaseID + "cicdrun/v1/step.schema.json"

// CICDStepSchema returns the JSON Schema bytes for cicdrunv1.Step.
func CICDStepSchema() ([]byte, error) {
	return reflectSchema(CICDStepSchemaID, "Eshu ci.step Payload (schema version 1)", &cicdrunv1.Step{})
}

// CICDWorkflowImageEvidenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "ci.workflow_image_evidence" payload.
const CICDWorkflowImageEvidenceSchemaID = schemaBaseID + "cicdrun/v1/workflow_image_evidence.schema.json"

// CICDWorkflowImageEvidenceSchema returns the JSON Schema bytes for
// cicdrunv1.WorkflowImageEvidence.
func CICDWorkflowImageEvidenceSchema() ([]byte, error) {
	return reflectSchema(CICDWorkflowImageEvidenceSchemaID, "Eshu ci.workflow_image_evidence Payload (schema version 1)", &cicdrunv1.WorkflowImageEvidence{})
}

// VaultAuthRoleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_auth_role" payload.
const VaultAuthRoleSchemaID = schemaBaseID + "secretsiam/v1/vault_auth_role.schema.json"

// VaultAuthRoleSchema returns the JSON Schema bytes for
// secretsiamv1.VaultAuthRole.
func VaultAuthRoleSchema() ([]byte, error) {
	return reflectSchema(VaultAuthRoleSchemaID, "Eshu vault_auth_role Payload (schema version 1)", &secretsiamv1.VaultAuthRole{})
}

// VaultACLPolicySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_acl_policy" payload.
const VaultACLPolicySchemaID = schemaBaseID + "secretsiam/v1/vault_acl_policy.schema.json"

// VaultACLPolicySchema returns the JSON Schema bytes for
// secretsiamv1.VaultACLPolicy.
func VaultACLPolicySchema() ([]byte, error) {
	return reflectSchema(VaultACLPolicySchemaID, "Eshu vault_acl_policy Payload (schema version 1)", &secretsiamv1.VaultACLPolicy{})
}

// VaultKVMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "vault_kv_metadata" payload.
const VaultKVMetadataSchemaID = schemaBaseID + "secretsiam/v1/vault_kv_metadata.schema.json"

// VaultKVMetadataSchema returns the JSON Schema bytes for
// secretsiamv1.VaultKVMetadata.
func VaultKVMetadataSchema() ([]byte, error) {
	return reflectSchema(VaultKVMetadataSchemaID, "Eshu vault_kv_metadata Payload (schema version 1)", &secretsiamv1.VaultKVMetadata{})
}

// KubernetesServiceAccountSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "k8s_service_account" payload.
const KubernetesServiceAccountSchemaID = schemaBaseID + "secretsiam/v1/k8s_service_account.schema.json"

// KubernetesServiceAccountSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesServiceAccount.
func KubernetesServiceAccountSchema() ([]byte, error) {
	return reflectSchema(KubernetesServiceAccountSchemaID, "Eshu k8s_service_account Payload (schema version 1)", &secretsiamv1.KubernetesServiceAccount{})
}

// KubernetesWorkloadIdentityUseSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "k8s_workload_identity_use" payload.
const KubernetesWorkloadIdentityUseSchemaID = schemaBaseID + "secretsiam/v1/k8s_workload_identity_use.schema.json"

// KubernetesWorkloadIdentityUseSchema returns the JSON Schema bytes for
// secretsiamv1.KubernetesWorkloadIdentityUse.
func KubernetesWorkloadIdentityUseSchema() ([]byte, error) {
	return reflectSchema(KubernetesWorkloadIdentityUseSchemaID, "Eshu k8s_workload_identity_use Payload (schema version 1)", &secretsiamv1.KubernetesWorkloadIdentityUse{})
}

// EKSIRSAAnnotationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "eks_irsa_annotation" payload.
const EKSIRSAAnnotationSchemaID = schemaBaseID + "secretsiam/v1/eks_irsa_annotation.schema.json"

// EKSIRSAAnnotationSchema returns the JSON Schema bytes for
// secretsiamv1.EKSIRSAAnnotation.
func EKSIRSAAnnotationSchema() ([]byte, error) {
	return reflectSchema(EKSIRSAAnnotationSchemaID, "Eshu eks_irsa_annotation Payload (schema version 1)", &secretsiamv1.EKSIRSAAnnotation{})
}

// EKSPodIdentityAssociationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "eks_pod_identity_association" payload.
const EKSPodIdentityAssociationSchemaID = schemaBaseID + "secretsiam/v1/eks_pod_identity_association.schema.json"

// EKSPodIdentityAssociationSchema returns the JSON Schema bytes for
// secretsiamv1.EKSPodIdentityAssociation.
func EKSPodIdentityAssociationSchema() ([]byte, error) {
	return reflectSchema(EKSPodIdentityAssociationSchemaID, "Eshu eks_pod_identity_association Payload (schema version 1)", &secretsiamv1.EKSPodIdentityAssociation{})
}

// KubernetesGCPWorkloadIdentityBindingSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "k8s_gcp_workload_identity_binding" payload.
const KubernetesGCPWorkloadIdentityBindingSchemaID = schemaBaseID + "secretsiam/v1/k8s_gcp_workload_identity_binding.schema.json"

// KubernetesGCPWorkloadIdentityBindingSchema returns the JSON Schema bytes
// for secretsiamv1.KubernetesGCPWorkloadIdentityBinding.
func KubernetesGCPWorkloadIdentityBindingSchema() ([]byte, error) {
	return reflectSchema(KubernetesGCPWorkloadIdentityBindingSchemaID, "Eshu k8s_gcp_workload_identity_binding Payload (schema version 1)", &secretsiamv1.KubernetesGCPWorkloadIdentityBinding{})
}

// WorkItemRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.record" payload.
const WorkItemRecordSchemaID = schemaBaseID + "workitem/v1/record.schema.json"

// WorkItemRecordSchema returns the JSON Schema bytes for
// workitemv1.WorkItemRecord.
func WorkItemRecordSchema() ([]byte, error) {
	return reflectSchema(WorkItemRecordSchemaID, "Eshu work_item.record Payload (schema version 1)", &workitemv1.WorkItemRecord{})
}

// WorkItemTransitionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.transition" payload.
const WorkItemTransitionSchemaID = schemaBaseID + "workitem/v1/transition.schema.json"

// WorkItemTransitionSchema returns the JSON Schema bytes for
// workitemv1.WorkItemTransition.
func WorkItemTransitionSchema() ([]byte, error) {
	return reflectSchema(WorkItemTransitionSchemaID, "Eshu work_item.transition Payload (schema version 1)", &workitemv1.WorkItemTransition{})
}

// WorkItemExternalLinkSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.external_link" payload.
const WorkItemExternalLinkSchemaID = schemaBaseID + "workitem/v1/external_link.schema.json"

// WorkItemExternalLinkSchema returns the JSON Schema bytes for
// workitemv1.WorkItemExternalLink.
func WorkItemExternalLinkSchema() ([]byte, error) {
	return reflectSchema(WorkItemExternalLinkSchemaID, "Eshu work_item.external_link Payload (schema version 1)", &workitemv1.WorkItemExternalLink{})
}

// WorkItemProjectMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.project_metadata" payload.
const WorkItemProjectMetadataSchemaID = schemaBaseID + "workitem/v1/project_metadata.schema.json"

// WorkItemProjectMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemProjectMetadata.
func WorkItemProjectMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemProjectMetadataSchemaID, "Eshu work_item.project_metadata Payload (schema version 1)", &workitemv1.WorkItemProjectMetadata{})
}

// WorkItemIssueTypeMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.issue_type_metadata" payload.
const WorkItemIssueTypeMetadataSchemaID = schemaBaseID + "workitem/v1/issue_type_metadata.schema.json"

// WorkItemIssueTypeMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemIssueTypeMetadata.
func WorkItemIssueTypeMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemIssueTypeMetadataSchemaID, "Eshu work_item.issue_type_metadata Payload (schema version 1)", &workitemv1.WorkItemIssueTypeMetadata{})
}

// WorkItemStatusMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.status_metadata" payload.
const WorkItemStatusMetadataSchemaID = schemaBaseID + "workitem/v1/status_metadata.schema.json"

// WorkItemStatusMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemStatusMetadata.
func WorkItemStatusMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemStatusMetadataSchemaID, "Eshu work_item.status_metadata Payload (schema version 1)", &workitemv1.WorkItemStatusMetadata{})
}

// WorkItemWorkflowMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.workflow_metadata" payload.
const WorkItemWorkflowMetadataSchemaID = schemaBaseID + "workitem/v1/workflow_metadata.schema.json"

// WorkItemWorkflowMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemWorkflowMetadata.
func WorkItemWorkflowMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemWorkflowMetadataSchemaID, "Eshu work_item.workflow_metadata Payload (schema version 1)", &workitemv1.WorkItemWorkflowMetadata{})
}

// WorkItemFieldMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.field_metadata" payload.
const WorkItemFieldMetadataSchemaID = schemaBaseID + "workitem/v1/field_metadata.schema.json"

// WorkItemFieldMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemFieldMetadata.
func WorkItemFieldMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemFieldMetadataSchemaID, "Eshu work_item.field_metadata Payload (schema version 1)", &workitemv1.WorkItemFieldMetadata{})
}

// WorkItemMetadataWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.metadata_warning" payload.
const WorkItemMetadataWarningSchemaID = schemaBaseID + "workitem/v1/metadata_warning.schema.json"

// WorkItemMetadataWarningSchema returns the JSON Schema bytes for
// workitemv1.WorkItemMetadataWarning.
func WorkItemMetadataWarningSchema() ([]byte, error) {
	return reflectSchema(WorkItemMetadataWarningSchemaID, "Eshu work_item.metadata_warning Payload (schema version 1)", &workitemv1.WorkItemMetadataWarning{})
}

// SecurityAlertRepositoryAlertSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "security_alert.repository_alert" payload.
const SecurityAlertRepositoryAlertSchemaID = schemaBaseID + "securityalert/v1/repository_alert.schema.json"

// SecurityAlertRepositoryAlertSchema returns the JSON Schema bytes for
// securityalertv1.RepositoryAlert.
func SecurityAlertRepositoryAlertSchema() ([]byte, error) {
	return reflectSchema(SecurityAlertRepositoryAlertSchemaID, "Eshu security_alert.repository_alert Payload (schema version 1)", &securityalertv1.RepositoryAlert{})
}

// ObservabilityDeclaredFolderSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_folder' payload.
const ObservabilityDeclaredFolderSchemaID = schemaBaseID + "observability/v1/declared_folder.schema.json"

// ObservabilityDeclaredFolderSchema returns the JSON Schema bytes for observabilityv1.DeclaredFolder.
func ObservabilityDeclaredFolderSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredFolderSchemaID, "Eshu observability.declared_folder Payload (schema version 1)", &observabilityv1.DeclaredFolder{})
}

// ObservabilityDeclaredDashboardSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_dashboard' payload.
const ObservabilityDeclaredDashboardSchemaID = schemaBaseID + "observability/v1/declared_dashboard.schema.json"

// ObservabilityDeclaredDashboardSchema returns the JSON Schema bytes for observabilityv1.DeclaredDashboard.
func ObservabilityDeclaredDashboardSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredDashboardSchemaID, "Eshu observability.declared_dashboard Payload (schema version 1)", &observabilityv1.DeclaredDashboard{})
}

// ObservabilityDeclaredDatasourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_datasource' payload.
const ObservabilityDeclaredDatasourceSchemaID = schemaBaseID + "observability/v1/declared_datasource.schema.json"

// ObservabilityDeclaredDatasourceSchema returns the JSON Schema bytes for observabilityv1.DeclaredDatasource.
func ObservabilityDeclaredDatasourceSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredDatasourceSchemaID, "Eshu observability.declared_datasource Payload (schema version 1)", &observabilityv1.DeclaredDatasource{})
}

// ObservabilityDeclaredAlertRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_alert_rule' payload.
const ObservabilityDeclaredAlertRuleSchemaID = schemaBaseID + "observability/v1/declared_alert_rule.schema.json"

// ObservabilityDeclaredAlertRuleSchema returns the JSON Schema bytes for observabilityv1.DeclaredAlertRule.
func ObservabilityDeclaredAlertRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredAlertRuleSchemaID, "Eshu observability.declared_alert_rule Payload (schema version 1)", &observabilityv1.DeclaredAlertRule{})
}

// ObservabilityDeclaredScrapeConfigSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_scrape_config' payload.
const ObservabilityDeclaredScrapeConfigSchemaID = schemaBaseID + "observability/v1/declared_scrape_config.schema.json"

// ObservabilityDeclaredScrapeConfigSchema returns the JSON Schema bytes for observabilityv1.DeclaredScrapeConfig.
func ObservabilityDeclaredScrapeConfigSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredScrapeConfigSchemaID, "Eshu observability.declared_scrape_config Payload (schema version 1)", &observabilityv1.DeclaredScrapeConfig{})
}

// ObservabilityDeclaredMetricRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_metric_rule' payload.
const ObservabilityDeclaredMetricRuleSchemaID = schemaBaseID + "observability/v1/declared_metric_rule.schema.json"

// ObservabilityDeclaredMetricRuleSchema returns the JSON Schema bytes for observabilityv1.DeclaredMetricRule.
func ObservabilityDeclaredMetricRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredMetricRuleSchemaID, "Eshu observability.declared_metric_rule Payload (schema version 1)", &observabilityv1.DeclaredMetricRule{})
}

// ObservabilityDeclaredMetricRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_metric_route' payload.
const ObservabilityDeclaredMetricRouteSchemaID = schemaBaseID + "observability/v1/declared_metric_route.schema.json"

// ObservabilityDeclaredMetricRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredMetricRoute.
func ObservabilityDeclaredMetricRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredMetricRouteSchemaID, "Eshu observability.declared_metric_route Payload (schema version 1)", &observabilityv1.DeclaredMetricRoute{})
}

// ObservabilityDeclaredLogRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_log_route' payload.
const ObservabilityDeclaredLogRouteSchemaID = schemaBaseID + "observability/v1/declared_log_route.schema.json"

// ObservabilityDeclaredLogRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredLogRoute.
func ObservabilityDeclaredLogRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredLogRouteSchemaID, "Eshu observability.declared_log_route Payload (schema version 1)", &observabilityv1.DeclaredLogRoute{})
}

// ObservabilityDeclaredTraceRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_trace_route' payload.
const ObservabilityDeclaredTraceRouteSchemaID = schemaBaseID + "observability/v1/declared_trace_route.schema.json"

// ObservabilityDeclaredTraceRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredTraceRoute.
func ObservabilityDeclaredTraceRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredTraceRouteSchemaID, "Eshu observability.declared_trace_route Payload (schema version 1)", &observabilityv1.DeclaredTraceRoute{})
}

// ObservabilityAppliedResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.applied_resource' payload.
const ObservabilityAppliedResourceSchemaID = schemaBaseID + "observability/v1/applied_resource.schema.json"

// ObservabilityAppliedResourceSchema returns the JSON Schema bytes for observabilityv1.AppliedResource.
func ObservabilityAppliedResourceSchema() ([]byte, error) {
	return reflectSchema(ObservabilityAppliedResourceSchemaID, "Eshu observability.applied_resource Payload (schema version 1)", &observabilityv1.AppliedResource{})
}

// ObservabilityAppliedSyncStateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.applied_sync_state' payload.
const ObservabilityAppliedSyncStateSchemaID = schemaBaseID + "observability/v1/applied_sync_state.schema.json"

// ObservabilityAppliedSyncStateSchema returns the JSON Schema bytes for observabilityv1.AppliedSyncState.
func ObservabilityAppliedSyncStateSchema() ([]byte, error) {
	return reflectSchema(ObservabilityAppliedSyncStateSchemaID, "Eshu observability.applied_sync_state Payload (schema version 1)", &observabilityv1.AppliedSyncState{})
}

// ObservabilityObservedDashboardSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_dashboard' payload.
const ObservabilityObservedDashboardSchemaID = schemaBaseID + "observability/v1/observed_dashboard.schema.json"

// ObservabilityObservedDashboardSchema returns the JSON Schema bytes for observabilityv1.ObservedDashboard.
func ObservabilityObservedDashboardSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedDashboardSchemaID, "Eshu observability.observed_dashboard Payload (schema version 1)", &observabilityv1.ObservedDashboard{})
}

// ObservabilityObservedTargetSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_target' payload.
const ObservabilityObservedTargetSchemaID = schemaBaseID + "observability/v1/observed_target.schema.json"

// ObservabilityObservedTargetSchema returns the JSON Schema bytes for observabilityv1.ObservedTarget.
func ObservabilityObservedTargetSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedTargetSchemaID, "Eshu observability.observed_target Payload (schema version 1)", &observabilityv1.ObservedTarget{})
}

// ObservabilityObservedRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_rule' payload.
const ObservabilityObservedRuleSchemaID = schemaBaseID + "observability/v1/observed_rule.schema.json"

// ObservabilityObservedRuleSchema returns the JSON Schema bytes for observabilityv1.ObservedRule.
func ObservabilityObservedRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedRuleSchemaID, "Eshu observability.observed_rule Payload (schema version 1)", &observabilityv1.ObservedRule{})
}

// ObservabilityObservedLogSignalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_log_signal' payload.
const ObservabilityObservedLogSignalSchemaID = schemaBaseID + "observability/v1/observed_log_signal.schema.json"

// ObservabilityObservedLogSignalSchema returns the JSON Schema bytes for observabilityv1.ObservedLogSignal.
func ObservabilityObservedLogSignalSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedLogSignalSchemaID, "Eshu observability.observed_log_signal Payload (schema version 1)", &observabilityv1.ObservedLogSignal{})
}

// ObservabilityObservedTraceSignalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_trace_signal' payload.
const ObservabilityObservedTraceSignalSchemaID = schemaBaseID + "observability/v1/observed_trace_signal.schema.json"

// ObservabilityObservedTraceSignalSchema returns the JSON Schema bytes for observabilityv1.ObservedTraceSignal.
func ObservabilityObservedTraceSignalSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedTraceSignalSchemaID, "Eshu observability.observed_trace_signal Payload (schema version 1)", &observabilityv1.ObservedTraceSignal{})
}

// ObservabilityCoverageWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.coverage_warning' payload.
const ObservabilityCoverageWarningSchemaID = schemaBaseID + "observability/v1/coverage_warning.schema.json"

// ObservabilityCoverageWarningSchema returns the JSON Schema bytes for observabilityv1.CoverageWarning.
func ObservabilityCoverageWarningSchema() ([]byte, error) {
	return reflectSchema(ObservabilityCoverageWarningSchemaID, "Eshu observability.coverage_warning Payload (schema version 1)", &observabilityv1.CoverageWarning{})
}

// ObservabilitySourceInstanceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.source_instance' payload.
const ObservabilitySourceInstanceSchemaID = schemaBaseID + "observability/v1/source_instance.schema.json"

// ObservabilitySourceInstanceSchema returns the JSON Schema bytes for observabilityv1.SourceInstance.
func ObservabilitySourceInstanceSchema() ([]byte, error) {
	return reflectSchema(ObservabilitySourceInstanceSchemaID, "Eshu observability.source_instance Payload (schema version 1)", &observabilityv1.SourceInstance{})
}

// DocumentationSourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_source" payload.
const DocumentationSourceSchemaID = schemaBaseID + "documentation/v1/source.schema.json"

// DocumentationSourceSchema returns the JSON Schema bytes for
// documentationv1.Source.
func DocumentationSourceSchema() ([]byte, error) {
	return reflectSchema(DocumentationSourceSchemaID, "Eshu documentation_source Payload (schema version 1)", &documentationv1.Source{})
}

// DocumentationDocumentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_document" payload.
const DocumentationDocumentSchemaID = schemaBaseID + "documentation/v1/document.schema.json"

// DocumentationDocumentSchema returns the JSON Schema bytes for
// documentationv1.Document.
func DocumentationDocumentSchema() ([]byte, error) {
	return reflectSchema(DocumentationDocumentSchemaID, "Eshu documentation_document Payload (schema version 1)", &documentationv1.Document{})
}

// DocumentationSectionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1.1.0 "documentation_section" payload.
const DocumentationSectionSchemaID = schemaBaseID + "documentation/v1/section.schema.json"

// DocumentationSectionSchema returns the JSON Schema bytes for
// documentationv1.Section. The title names schema version 1.1.0
// (facts.DocumentationSectionFactSchemaVersion) because this kind is one
// minor ahead of the rest of the documentation family (which is 1.0.0); the
// decode seam still dispatches on the schema-version major only, mirroring
// gcp_cloud_resource's identical one-minor-ahead precedent.
func DocumentationSectionSchema() ([]byte, error) {
	return reflectSchema(DocumentationSectionSchemaID, "Eshu documentation_section Payload (schema version 1.1.0)", &documentationv1.Section{})
}

// DocumentationLinkSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_link" payload.
const DocumentationLinkSchemaID = schemaBaseID + "documentation/v1/link.schema.json"

// DocumentationLinkSchema returns the JSON Schema bytes for
// documentationv1.Link.
func DocumentationLinkSchema() ([]byte, error) {
	return reflectSchema(DocumentationLinkSchemaID, "Eshu documentation_link Payload (schema version 1)", &documentationv1.Link{})
}

// DocumentationEntityMentionSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_entity_mention" payload.
const DocumentationEntityMentionSchemaID = schemaBaseID + "documentation/v1/entity_mention.schema.json"

// DocumentationEntityMentionSchema returns the JSON Schema bytes for
// documentationv1.EntityMention.
func DocumentationEntityMentionSchema() ([]byte, error) {
	return reflectSchema(DocumentationEntityMentionSchemaID, "Eshu documentation_entity_mention Payload (schema version 1)", &documentationv1.EntityMention{})
}

// DocumentationClaimCandidateSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_claim_candidate" payload.
const DocumentationClaimCandidateSchemaID = schemaBaseID + "documentation/v1/claim_candidate.schema.json"

// DocumentationClaimCandidateSchema returns the JSON Schema bytes for
// documentationv1.ClaimCandidate.
func DocumentationClaimCandidateSchema() ([]byte, error) {
	return reflectSchema(DocumentationClaimCandidateSchemaID, "Eshu documentation_claim_candidate Payload (schema version 1)", &documentationv1.ClaimCandidate{})
}

// DocumentationFindingSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_finding" payload.
const DocumentationFindingSchemaID = schemaBaseID + "documentation/v1/finding.schema.json"

// DocumentationFindingSchema returns the JSON Schema bytes for
// documentationv1.Finding.
func DocumentationFindingSchema() ([]byte, error) {
	return reflectSchema(DocumentationFindingSchemaID, "Eshu documentation_finding Payload (schema version 1)", &documentationv1.Finding{})
}

// DocumentationEvidencePacketSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_evidence_packet" payload.
const DocumentationEvidencePacketSchemaID = schemaBaseID + "documentation/v1/evidence_packet.schema.json"

// DocumentationEvidencePacketSchema returns the JSON Schema bytes for
// documentationv1.EvidencePacket.
func DocumentationEvidencePacketSchema() ([]byte, error) {
	return reflectSchema(DocumentationEvidencePacketSchemaID, "Eshu documentation_evidence_packet Payload (schema version 1)", &documentationv1.EvidencePacket{})
}

// ServiceCatalogEntitySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "service_catalog.entity" payload.
const ServiceCatalogEntitySchemaID = schemaBaseID + "servicecatalog/v1/entity.schema.json"

// ServiceCatalogEntitySchema returns the JSON Schema bytes for
// servicecatalogv1.Entity.
func ServiceCatalogEntitySchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogEntitySchemaID, "Eshu service_catalog.entity Payload (schema version 1)", &servicecatalogv1.Entity{})
}

// ServiceCatalogOwnershipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "service_catalog.ownership" payload.
const ServiceCatalogOwnershipSchemaID = schemaBaseID + "servicecatalog/v1/ownership.schema.json"

// ServiceCatalogOwnershipSchema returns the JSON Schema bytes for
// servicecatalogv1.Ownership.
func ServiceCatalogOwnershipSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogOwnershipSchemaID, "Eshu service_catalog.ownership Payload (schema version 1)", &servicecatalogv1.Ownership{})
}

// ServiceCatalogRepositoryLinkSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "service_catalog.repository_link" payload.
const ServiceCatalogRepositoryLinkSchemaID = schemaBaseID + "servicecatalog/v1/repository_link.schema.json"

// ServiceCatalogRepositoryLinkSchema returns the JSON Schema bytes for
// servicecatalogv1.RepositoryLink.
func ServiceCatalogRepositoryLinkSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogRepositoryLinkSchemaID, "Eshu service_catalog.repository_link Payload (schema version 1)", &servicecatalogv1.RepositoryLink{})
}

// ServiceCatalogOperationalLinkSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "service_catalog.operational_link" payload.
const ServiceCatalogOperationalLinkSchemaID = schemaBaseID + "servicecatalog/v1/operational_link.schema.json"

// ServiceCatalogOperationalLinkSchema returns the JSON Schema bytes for
// servicecatalogv1.OperationalLink.
func ServiceCatalogOperationalLinkSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogOperationalLinkSchemaID, "Eshu service_catalog.operational_link Payload (schema version 1)", &servicecatalogv1.OperationalLink{})
}
