// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/internal/schemagen"
)

// TestSchemasHaveNoDrift regenerates every fact kind's JSON Schema in memory and
// asserts it is byte-identical to the checked-in artifact under schema/. This
// makes schema drift a `go test` failure rather than something only the
// schema-diff CI gate would catch: if a typed payload struct changes without
// re-running `go generate ./...`, this test fails until the committed schema is
// regenerated. Every new fact kind MUST add a row here so its schema is
// drift-locked to its struct like the others.
func TestSchemasHaveNoDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file     string
		generate func() ([]byte, error)
	}{
		{file: "aws_resource.v1.schema.json", generate: schemagen.AWSResourceSchema},
		{file: "aws_relationship.v1.schema.json", generate: schemagen.AWSRelationshipSchema},
		{file: "aws_security_group_rule.v1.schema.json", generate: schemagen.AWSSecurityGroupRuleSchema},
		{file: "ec2_instance_posture.v1.schema.json", generate: schemagen.EC2InstancePostureSchema},
		{file: "s3_bucket_posture.v1.schema.json", generate: schemagen.S3BucketPostureSchema},
		{file: "aws_iam_permission.v1.schema.json", generate: schemagen.AWSIAMPermissionSchema},
		{file: "aws_resource_policy_permission.v1.schema.json", generate: schemagen.AWSResourcePolicyPermissionSchema},
		{file: "aws_iam_principal.v1.schema.json", generate: schemagen.AWSIAMPrincipalSchema},
		{file: "gcp_cloud_resource.v1.schema.json", generate: schemagen.GCPCloudResourceSchema},
		{file: "gcp_cloud_relationship.v1.schema.json", generate: schemagen.GCPCloudRelationshipSchema},
		{file: "gcp_collection_warning.v1.schema.json", generate: schemagen.GCPCollectionWarningSchema},
		{file: "gcp_dns_record.v1.schema.json", generate: schemagen.GCPDNSRecordSchema},
		{file: "gcp_iam_policy_observation.v1.schema.json", generate: schemagen.GCPIAMPolicyObservationSchema},
		{file: "kubernetes_live.pod_template.v1.schema.json", generate: schemagen.KubernetesLivePodTemplateSchema},
		{file: "kubernetes_live.relationship.v1.schema.json", generate: schemagen.KubernetesLiveRelationshipSchema},
		{file: "kubernetes_live.warning.v1.schema.json", generate: schemagen.KubernetesLiveWarningSchema},
		{file: "oci_registry.repository.v1.schema.json", generate: schemagen.OCIRegistryRepositorySchema},
		{file: "oci_registry.image_manifest.v1.schema.json", generate: schemagen.OCIImageManifestSchema},
		{file: "oci_registry.image_index.v1.schema.json", generate: schemagen.OCIImageIndexSchema},
		{file: "oci_registry.image_descriptor.v1.schema.json", generate: schemagen.OCIImageDescriptorSchema},
		{file: "oci_registry.image_tag_observation.v1.schema.json", generate: schemagen.OCIImageTagObservationSchema},
		{file: "oci_registry.image_referrer.v1.schema.json", generate: schemagen.OCIImageReferrerSchema},
		{file: "oci_registry.warning.v1.schema.json", generate: schemagen.OCIRegistryWarningSchema},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			got, err := tc.generate()
			if err != nil {
				t.Fatalf("generate %s error = %v, want nil", tc.file, err)
			}

			want, err := os.ReadFile(filepath.Join("schema", tc.file))
			if err != nil {
				t.Fatalf("os.ReadFile(schema/%s) error = %v, want nil", tc.file, err)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("generated %s drifted from committed artifact; run `go generate ./...` in sdk/go/factschema and commit the result\n\ngenerated:\n%s\n\ncommitted:\n%s", tc.file, got, want)
			}
		})
	}
}

// TestSchemasMatchCollectorPayloadShape locks the openness and nullability
// contract every fact-kind schema must hold so it validates the REAL collector
// payload, not just the reducer-consumed typed subset. The collectors emit extra
// context/service keys the reducer ignores (collector_instance_id, service_kind,
// service-specific fields, the nested attributes object) and explicit JSON null
// for absent optionals (boolOrNil / int32OrNil / a nil pointer). So for every
// kind:
//   - the top-level object MUST be open (additionalProperties: true), and
//   - every optional property (not in "required") MUST accept null.
//
// A schema that is additionalProperties:false or types an optional non-nullable
// would reject a real emitted payload — the wrong committed contract this test
// prevents (it fails if the generator's post-processing is dropped).
func TestSchemasMatchCollectorPayloadShape(t *testing.T) {
	t.Parallel()

	files := []string{
		"aws_resource.v1.schema.json",
		"aws_relationship.v1.schema.json",
		"aws_security_group_rule.v1.schema.json",
		"ec2_instance_posture.v1.schema.json",
		"s3_bucket_posture.v1.schema.json",
		"aws_iam_permission.v1.schema.json",
		"aws_resource_policy_permission.v1.schema.json",
		"aws_iam_principal.v1.schema.json",
		"gcp_cloud_resource.v1.schema.json",
		"gcp_cloud_relationship.v1.schema.json",
		"gcp_collection_warning.v1.schema.json",
		"gcp_dns_record.v1.schema.json",
		"gcp_iam_policy_observation.v1.schema.json",
		"kubernetes_live.pod_template.v1.schema.json",
		"kubernetes_live.relationship.v1.schema.json",
		"kubernetes_live.warning.v1.schema.json",
		"oci_registry.repository.v1.schema.json",
		"oci_registry.image_manifest.v1.schema.json",
		"oci_registry.image_index.v1.schema.json",
		"oci_registry.image_descriptor.v1.schema.json",
		"oci_registry.image_tag_observation.v1.schema.json",
		"oci_registry.image_referrer.v1.schema.json",
		"oci_registry.warning.v1.schema.json",
	}

	for _, file := range files {
		file := file
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("schema", file))
			if err != nil {
				t.Fatalf("os.ReadFile(schema/%s) error = %v, want nil", file, err)
			}
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("unmarshal schema/%s error = %v", file, err)
			}

			if open, _ := schema["additionalProperties"].(bool); !open {
				t.Fatalf("schema/%s top-level additionalProperties = %v, want true; the collector payload carries context/service keys the reducer does not consume", file, schema["additionalProperties"])
			}

			required := map[string]struct{}{}
			if rawRequired, ok := schema["required"].([]any); ok {
				for _, r := range rawRequired {
					if name, isString := r.(string); isString {
						required[name] = struct{}{}
					}
				}
			}

			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("schema/%s has no properties object", file)
			}
			for name, rawProp := range props {
				if _, isRequired := required[name]; isRequired {
					continue
				}
				prop, ok := rawProp.(map[string]any)
				if !ok {
					continue
				}
				if !typeAcceptsNull(prop["type"]) {
					t.Fatalf("schema/%s optional property %q type = %v, want it to accept null; the collector emits explicit null for an absent optional", file, name, prop["type"])
				}
			}
		})
	}
}

// typeAcceptsNull reports whether a JSON Schema "type" value permits an explicit
// null: a bare "null", a union array containing "null", or an absent type (an
// untyped open object already accepts null).
func typeAcceptsNull(t any) bool {
	switch typed := t.(type) {
	case nil:
		return true
	case string:
		return typed == "null"
	case []any:
		for _, v := range typed {
			if v == "null" {
				return true
			}
		}
		return false
	default:
		return false
	}
}
