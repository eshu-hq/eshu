// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen //nolint:filelength // per-family schema-generator registry; one SchemaID const + Schema func pair per migrated fact kind, reviewed as a single generator table. Splitting per-family is a separate refactor.

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
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
