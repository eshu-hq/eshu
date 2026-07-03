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

// TestAWSResourceSchemaHasNoDrift regenerates the aws_resource JSON Schema
// in memory and asserts it is byte-identical to the checked-in artifact at
// schema/aws_resource.v1.schema.json. This makes schema drift a `go test`
// failure rather than something only the schema-diff CI gate (out of scope
// for this scaffold) would catch: if awsv1.Resource changes without
// re-running `go generate ./...`, this test fails until the committed
// schema is regenerated.
func TestAWSResourceSchemaHasNoDrift(t *testing.T) {
	t.Parallel()

	got, err := schemagen.AWSResourceSchema()
	if err != nil {
		t.Fatalf("schemagen.AWSResourceSchema() error = %v, want nil", err)
	}

	want, err := os.ReadFile(filepath.Join("schema", "aws_resource.v1.schema.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(schema/aws_resource.v1.schema.json) error = %v, want nil", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("generated aws_resource schema drifted from committed artifact; run `go generate ./...` in sdk/go/factschema and commit the result\n\ngenerated:\n%s\n\ncommitted:\n%s", got, want)
	}
}
