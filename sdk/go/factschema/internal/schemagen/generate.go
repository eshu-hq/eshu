// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// AWSResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws.resource" payload.
const AWSResourceSchemaID = "https://eshu.dev/schemas/factschema/aws/v1/resource.schema.json"

// AWSResourceSchema returns the canonical, deterministically ordered JSON
// Schema bytes for awsv1.Resource. Both the generator's go:generate target
// (regenerate.go) and schema_gen_test.go's drift check call this function,
// so a generated artifact and its drift test can never disagree about how
// the schema is built.
//
// The reflector runs with DoNotReference so the single flat struct inlines
// directly instead of producing a $defs/$ref indirection, and with the
// default RequiredFromJSONSchemaTags=false so "required" is derived from
// Go's own pointer/omitempty shape (Contract System v1 §3.1): a struct
// field is required in the generated schema exactly when it is a
// non-pointer, non-map type with no `omitempty` json tag, matching the rule
// decode.go's requiredFields table encodes independently.
func AWSResourceSchema() ([]byte, error) {
	reflector := &jsonschema.Reflector{
		DoNotReference: true,
	}

	schema := reflector.Reflect(&awsv1.Resource{})
	schema.ID = jsonschema.ID(AWSResourceSchemaID)
	schema.Title = "Eshu aws.resource Payload (schema version 1)"

	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schemagen: marshal aws.resource schema: %w", err)
	}

	return append(raw, '\n'), nil
}
