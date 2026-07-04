// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

func testEnvelope(payload map[string]any) Envelope {
	return Envelope{
		FactKind:         FactKindAWSResource,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "arn:aws:s3:::example-bucket",
		ScopeID:          "aws-account:111111111111",
		GenerationID:     "gen-1",
		CollectorKind:    "aws-cloud-collector",
		SourceConfidence: "observed",
		ObservedAt:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		IsTombstone:      false,
		SourceRef:        "s3://example-bucket",
		Payload:          payload,
	}
}

func fullAWSResourcePayload() map[string]any {
	return map[string]any{
		"account_id":    "111111111111",
		"resource_id":   "arn:aws:s3:::example-bucket",
		"region":        "us-east-1",
		"resource_type": "aws.s3.bucket",
		"name":          "example-bucket",
		"tags":          map[string]any{"env": "prod"},
	}
}

// TestDecodeAWSResource_MissingRequiredField proves that a payload missing a
// required field ("region" is absent from the map, not merely empty) yields
// a classified error naming the field, never a zero-value struct. This is
// the accuracy backstop Contract System v1 §3.2 describes: a missing
// required field becomes an input_invalid dead letter, never a silent
// empty-string graph identity.
func TestDecodeAWSResource_MissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	delete(payload, "region") // absent, not empty-string present

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for missing required field")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeAWSResource() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "region" {
		t.Fatalf("Field = %q, want %q", classified.Field, "region")
	}

	var zero awsv1.Resource
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeAWSResource() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty
// proves the "missing" classification fires only when the required JSON key
// is absent from the payload map, not merely present with an empty value —
// an empty string is a valid (if unusual) observed value and must decode
// successfully.
func TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	payload["region"] = "" // present, but empty

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil for present-but-empty required field", err)
	}
	if got.Region != "" {
		t.Fatalf("Region = %q, want empty string", got.Region)
	}
}

// TestDecodeAWSResource_NullRequiredField proves that a required key present
// with an explicit JSON null (Go nil in the payload map) is rejected as a
// classified error, not silently unmarshaled to a zero value. Without this,
// json.Unmarshal turns null into "" for a string field with no error — the
// exact silent-zero-value identity this module exists to prevent. This is
// distinct from an empty string, which is a valid observed value (see
// TestDecodeAWSResource_MissingRequiredField_DistinguishesAbsentFromEmpty).
func TestDecodeAWSResource_NullRequiredField(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	payload["region"] = nil // present, but explicit JSON null

	got, err := DecodeAWSResource(testEnvelope(payload))
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for null required field")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeAWSResource() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "region" {
		t.Fatalf("Field = %q, want %q", classified.Field, "region")
	}

	var zero awsv1.Resource
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeAWSResource() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeAWSResource_RoundTrip proves that a typed struct encoded into an
// envelope payload map decodes back, via the kind-keyed seam, to a
// deep-equal copy of the original struct.
func TestDecodeAWSResource_RoundTrip(t *testing.T) {
	t.Parallel()

	name := "example-bucket"
	tags := map[string]string{"env": "prod"}
	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
		Name:         &name,
		Tags:         &tags,
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_RoundTrip_ObservedEmptyTags proves the "observed, no
// tags" state survives a round trip: a non-nil pointer to an empty map
// marshals as "tags":{} and decodes back to a non-nil pointer to an empty
// map, never collapsing to nil (which would be indistinguishable from "not
// observed"). This is the state the Tags pointer type exists to preserve.
func TestDecodeAWSResource_RoundTrip_ObservedEmptyTags(t *testing.T) {
	t.Parallel()

	emptyTags := map[string]string{}
	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
		Tags:         &emptyTags,
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}
	if _, ok := payload["tags"]; !ok {
		t.Fatalf("EncodeAWSResource() omitted an observed empty tags map; payload = %v", payload)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if decoded.Tags == nil {
		t.Fatalf("Tags = nil, want non-nil pointer to empty map (observed empty must not collapse to not-observed)")
	}
	if len(*decoded.Tags) != 0 {
		t.Fatalf("*Tags = %v, want empty map", *decoded.Tags)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_RoundTrip_OptionalFieldsAbsent proves the round trip
// also holds when optional fields are omitted entirely, leaving the decoded
// struct's pointer/map fields nil rather than defaulted.
func TestDecodeAWSResource_RoundTrip_OptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "arn:aws:s3:::example-bucket",
		Region:       "us-east-1",
		ResourceType: "aws.s3.bucket",
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if decoded.Name != nil {
		t.Fatalf("Name = %v, want nil", decoded.Name)
	}
	if decoded.Tags != nil {
		t.Fatalf("Tags = %v, want nil", decoded.Tags)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeAWSResource_UnsupportedMajor proves an unsupported schema-version
// major is a classified decode error, not a silent best-effort decode.
func TestDecodeAWSResource_UnsupportedMajor(t *testing.T) {
	t.Parallel()

	env := testEnvelope(fullAWSResourcePayload())
	env.SchemaVersion = "2.0.0"

	_, err := DecodeAWSResource(env)
	if err == nil {
		t.Fatalf("DecodeAWSResource() error = nil, want non-nil for unsupported major")
	}
	if !errors.Is(err, ErrUnsupportedSchemaMajor) {
		t.Fatalf("DecodeAWSResource() error = %v, want errors.Is ErrUnsupportedSchemaMajor", err)
	}
}

// payloadContracts is the registry of every typed fact-kind payload and its
// checked-in JSON Schema. The drift tests below iterate it, so adding a new
// fact kind means adding one row here — and TestPayloadContractsCoverAllSchemas
// makes forgetting that row a test failure rather than a silent coverage gap.
var payloadContracts = []struct {
	// factKind is the fact kind identifier, used only for test messages.
	factKind string
	// schemaFile is the schema's filename under schema/.
	schemaFile string
	// typ is the payload struct type whose reflectively derived key set must
	// match the generated schema.
	typ reflect.Type
}{
	{FactKindAWSResource, "aws_resource.v1.schema.json", reflect.TypeOf(awsv1.Resource{})},
	{FactKindAWSRelationship, "aws_relationship.v1.schema.json", reflect.TypeOf(awsv1.Relationship{})},
	{FactKindAWSSecurityGroupRule, "aws_security_group_rule.v1.schema.json", reflect.TypeOf(awsv1.SecurityGroupRule{})},
	{FactKindEC2InstancePosture, "ec2_instance_posture.v1.schema.json", reflect.TypeOf(awsv1.EC2InstancePosture{})},
	{FactKindS3BucketPosture, "s3_bucket_posture.v1.schema.json", reflect.TypeOf(awsv1.S3BucketPosture{})},
	{FactKindAWSIAMPermission, "aws_iam_permission.v1.schema.json", reflect.TypeOf(iamv1.Permission{})},
	{FactKindAWSResourcePolicyPermission, "aws_resource_policy_permission.v1.schema.json", reflect.TypeOf(iamv1.ResourcePolicyPermission{})},
	{FactKindAWSIAMPrincipal, "aws_iam_principal.v1.schema.json", reflect.TypeOf(iamv1.Principal{})},
	{FactKindIncidentRecord, "incident.record.v1.schema.json", reflect.TypeOf(incidentv1.IncidentRecord{})},
	{FactKindIncidentLifecycleEvent, "incident.lifecycle_event.v1.schema.json", reflect.TypeOf(incidentv1.LifecycleEvent{})},
	{FactKindChangeRecord, "change.record.v1.schema.json", reflect.TypeOf(incidentv1.ChangeRecord{})},
	{FactKindIncidentRoutingAppliedPagerDutyResource, "incident_routing.applied_pagerduty_resource.v1.schema.json", reflect.TypeOf(incidentv1.AppliedPagerDutyResource{})},
	{FactKindIncidentRoutingAppliedAlertRoute, "incident_routing.applied_alert_route.v1.schema.json", reflect.TypeOf(incidentv1.AppliedAlertRoute{})},
	{FactKindIncidentRoutingObservedPagerDutyService, "incident_routing.observed_pagerduty_service.v1.schema.json", reflect.TypeOf(incidentv1.ObservedPagerDutyService{})},
	{FactKindIncidentRoutingObservedPagerDutyIntegration, "incident_routing.observed_pagerduty_integration.v1.schema.json", reflect.TypeOf(incidentv1.ObservedPagerDutyIntegration{})},
	{FactKindIncidentRoutingCoverageWarning, "incident_routing.coverage_warning.v1.schema.json", reflect.TypeOf(incidentv1.CoverageWarning{})},
	{FactKindGCPCloudResource, "gcp_cloud_resource.v1.schema.json", reflect.TypeOf(gcpv1.Resource{})},
	{FactKindGCPCloudRelationship, "gcp_cloud_relationship.v1.schema.json", reflect.TypeOf(gcpv1.Relationship{})},
	{FactKindGCPCollectionWarning, "gcp_collection_warning.v1.schema.json", reflect.TypeOf(gcpv1.CollectionWarning{})},
	{FactKindGCPDNSRecord, "gcp_dns_record.v1.schema.json", reflect.TypeOf(gcpv1.DNSRecord{})},
	{FactKindGCPIAMPolicyObservation, "gcp_iam_policy_observation.v1.schema.json", reflect.TypeOf(gcpv1.IAMPolicyObservation{})},
	{FactKindAzureCloudResource, "azure_cloud_resource.v1.schema.json", reflect.TypeOf(azurev1.CloudResource{})},
	{FactKindAzureCloudRelationship, "azure_cloud_relationship.v1.schema.json", reflect.TypeOf(azurev1.CloudRelationship{})},
	{FactKindAzureDNSRecord, "azure_dns_record.v1.schema.json", reflect.TypeOf(azurev1.DNSRecord{})},
	{FactKindAzureCollectionWarning, "azure_collection_warning.v1.schema.json", reflect.TypeOf(azurev1.CollectionWarning{})},
	{FactKindKubernetesLivePodTemplate, "kubernetes_live.pod_template.v1.schema.json", reflect.TypeOf(kuberneteslivev1.PodTemplate{})},
	{FactKindKubernetesLiveRelationship, "kubernetes_live.relationship.v1.schema.json", reflect.TypeOf(kuberneteslivev1.Relationship{})},
	{FactKindKubernetesLiveWarning, "kubernetes_live.warning.v1.schema.json", reflect.TypeOf(kuberneteslivev1.Warning{})},
	{FactKindOCIRegistryRepository, "oci_registry.repository.v1.schema.json", reflect.TypeOf(ociregistryv1.Repository{})},
	{FactKindOCIImageManifest, "oci_registry.image_manifest.v1.schema.json", reflect.TypeOf(ociregistryv1.ImageManifest{})},
	{FactKindOCIImageIndex, "oci_registry.image_index.v1.schema.json", reflect.TypeOf(ociregistryv1.ImageIndex{})},
	{FactKindOCIImageDescriptor, "oci_registry.image_descriptor.v1.schema.json", reflect.TypeOf(ociregistryv1.ImageDescriptor{})},
	{FactKindOCIImageTagObservation, "oci_registry.image_tag_observation.v1.schema.json", reflect.TypeOf(ociregistryv1.TagObservation{})},
	{FactKindOCIImageReferrer, "oci_registry.image_referrer.v1.schema.json", reflect.TypeOf(ociregistryv1.ImageReferrer{})},
	{FactKindOCIRegistryWarning, "oci_registry.warning.v1.schema.json", reflect.TypeOf(ociregistryv1.Warning{})},
}

// TestPayloadContractsCoverAllSchemas fails if the payloadContracts registry
// and the checked-in schema/ directory disagree about which fact kinds exist:
// a schema file with no registry row (a kind added without wiring its drift
// coverage) or a registry row naming a missing schema file. This is the guard
// that keeps "add a kind" from silently skipping the single-source-of-truth
// checks the other tests enforce.
func TestPayloadContractsCoverAllSchemas(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir("schema")
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}
	schemaFiles := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		schemaFiles[entry.Name()] = true
	}

	registered := map[string]bool{}
	for _, contract := range payloadContracts {
		if registered[contract.schemaFile] {
			t.Fatalf("payloadContracts registers %q more than once", contract.schemaFile)
		}
		registered[contract.schemaFile] = true
		if !schemaFiles[contract.schemaFile] {
			t.Fatalf("payloadContracts row for %q names schema file %q, which does not exist under schema/", contract.factKind, contract.schemaFile)
		}
	}
	for name := range schemaFiles {
		if !registered[name] {
			t.Fatalf("schema file %q has no payloadContracts row; add one so its key set is drift-checked", name)
		}
	}
}

// TestDerivedKeySetsMatchGeneratedSchemas is the definitive single-source-of-
// truth lock: for each registered fact kind it derives the required and known
// key sets from the payload struct (via the same payloadKeySetOf the decode
// path uses) and asserts they equal the generated schema's "required" array
// and "properties" keys. Because the schema is generated from the same struct,
// the two agree automatically unless the two derivation rules diverge — for
// example an invopop upgrade changing its required semantics, or a change to
// parseJSONTag. That divergence, not a hand-map edit, is the only remaining
// drift axis, and this test is what catches it.
func TestDerivedKeySetsMatchGeneratedSchemas(t *testing.T) {
	t.Parallel()

	for _, contract := range payloadContracts {
		t.Run(contract.factKind, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("schema", contract.schemaFile))
			if err != nil {
				t.Fatalf("read schema %q: %v", contract.schemaFile, err)
			}
			var doc struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("unmarshal schema %q: %v", contract.schemaFile, err)
			}

			ks := payloadKeySetOf(contract.typ)

			schemaProperties := map[string]bool{}
			for name := range doc.Properties {
				schemaProperties[name] = true
			}
			if got := keySet(ks.Known); !reflect.DeepEqual(got, schemaProperties) {
				t.Fatalf("derived known keys = %v, want schema properties %v", sortedKeys(got), sortedKeys(schemaProperties))
			}

			schemaRequired := map[string]bool{}
			for _, name := range doc.Required {
				schemaRequired[name] = true
			}
			if got := keySet(ks.Required); !reflect.DeepEqual(got, schemaRequired) {
				t.Fatalf("derived required keys = %v, want schema required %v", sortedKeys(got), sortedKeys(schemaRequired))
			}
		})
	}
}

// TestPayloadStructShapeConvention enforces the two struct-shape bans that keep
// the required rule ("no omitempty ⇒ required") unambiguous per field. A
// pointer field without omitempty is required by the schema yet nullable, a
// required-but-nullable contradiction decodeAndValidate would reject at runtime
// as a null required field. A non-pointer, non-slice, non-map field with
// omitempty collapses the absent and zero-value states, discarding the
// observed/not-observed distinction the pointer-and-omitempty optional fields
// exist to preserve. Slice and map fields are exempt from that second ban: a
// nil slice/map already round-trips through omitempty exactly like a nil
// pointer (encoding/json omits a nil or empty slice/map with omitempty and a
// json.Unmarshal of an absent key leaves it nil), so a []string field tagged
// `json:"x,omitempty"` is not ambiguous the way a bare string field tagged
// `json:"x,omitempty"` would be. Banning the pointer and scalar shapes means
// the schema generator's "no omitempty ⇒ required" rule and the intuition
// "pointer/slice/map ⇒ optional" can never disagree.
func TestPayloadStructShapeConvention(t *testing.T) {
	t.Parallel()

	for _, contract := range payloadContracts {
		t.Run(contract.factKind, func(t *testing.T) {
			t.Parallel()

			typ := contract.typ
			seen := map[string]string{} // json name -> first Go field that declared it
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.Anonymous {
					t.Fatalf("field %q is embedded; payload structs must be flat", field.Name)
				}
				if field.PkgPath != "" {
					continue
				}
				name, omitEmpty, skip := parseJSONTag(field.Tag.Get("json"), field.Name)
				if skip {
					continue
				}
				if prior, dup := seen[name]; dup {
					t.Fatalf("fields %q and %q both serialize to json key %q; payload key names must be unique", prior, field.Name, name)
				}
				seen[name] = field.Name
				switch field.Type.Kind() {
				case reflect.Pointer:
					if !omitEmpty {
						t.Fatalf("field %q is a pointer without omitempty (required-but-nullable); add omitempty or make it a value type", field.Name)
					}
				case reflect.Slice, reflect.Map:
					// Nil is both the zero value and the absent-key decode
					// result for a slice/map, so omitempty does not collapse
					// a distinction the way it would for a scalar; either
					// tagging is unambiguous. Most slice/map fields carry
					// omitempty by convention (see aws/v1, iam/v1) and are
					// therefore optional. A slice/map WITHOUT omitempty is a
					// required collection: the schema lists it in "required" and
					// the decode seam dead-letters an absent or null key. That is
					// only correct when the emitter unconditionally writes the
					// key, so it must be opted in explicitly via
					// intentionalRequiredCollections; a required collection not on
					// that list is a bug (a field that would dead-letter a valid
					// fact the emitter can produce without it).
					_, allowed := intentionalRequiredCollections[requiredCollectionKey{contract.factKind, name}]
					if !omitEmpty && !allowed {
						t.Fatalf("field %q is a required slice/map (no omitempty) but is not in intentionalRequiredCollections; add it there with a justification or add omitempty", field.Name)
					}
				default:
					if omitEmpty {
						t.Fatalf("field %q is a non-pointer with omitempty (collapses absent and zero value); make it a pointer or drop omitempty", field.Name)
					}
				}
			}
		})
	}
}

// BenchmarkDecodeAWSResource records the baseline cost of the current
// json.Marshal/Unmarshal decode path so any future move to a lower-allocation
// decoder is justified by a before/after measurement rather than intuition.
func BenchmarkDecodeAWSResource(b *testing.B) {
	env := testEnvelope(fullAWSResourcePayload())
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeAWSResource(env); err != nil {
			b.Fatalf("DecodeAWSResource() error = %v", err)
		}
	}
}

// keySet collects a slice of key names into a set so the drift test can
// compare the derived keys against the generated schema's keys order-
// independently. Duplicate json names are caught separately, by
// TestPayloadStructShapeConvention, not here.
func keySet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
