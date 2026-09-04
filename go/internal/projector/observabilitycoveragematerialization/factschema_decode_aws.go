// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragematerialization

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// decodeAWSResource decodes an aws_resource envelope into the typed
// awsv1.Resource struct through the contracts seam.
//
// This package keeps its own decode rather than importing root's wrapper, the
// same way internal/projector/ec2 and the sibling
// internal/projector/observabilitycoverage do: sharing root's would require
// importing the package that already imports this one to dispatch, which
// cycles.
//
// The filename is not incidental. The payload-usage gate globs
// factschema_decode*.go recursively and AST-scans each function body for a
// factschema.FactKindXxx reference, so a decode living under any other name is
// invisible to it.
func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.Resource{}, fmt.Errorf("decode %s: %w", factschema.FactKindAWSResource, err)
	}
	return resource, nil
}
