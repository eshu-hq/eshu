// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
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
