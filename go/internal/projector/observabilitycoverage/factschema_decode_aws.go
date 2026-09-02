// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoverage

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds this family's decode wrapper for the aws_resource fact
// kind, named factschema_decode_aws.go to match the repo-wide convention
// (root's go/internal/projector/factschema_decode_aws.go,
// go/internal/projector/ec2/factschema_decode_aws.go) so the payload-usage
// manifest gate (scripts/verify-payload-usage-manifest.sh, issue #4573)
// discovers it: that gate globs factschema_decode*.go files recursively and
// AST-scans each function body for a factschema.FactKindXxx reference to
// recognize it as a decode seam.

// decodeObservabilityAWSResource decodes one aws_resource envelope into the
// typed awsv1.Resource struct through the contracts seam. This family package
// keeps its own decode call rather than importing root projector's
// classified-decode wrapper (root's decodeAWSResource in its
// factschema_decode_aws.go): sharing it would require this package to import
// root, which root already imports to dispatch to this package — an import
// cycle. The name carries the family prefix because the payload-usage
// manifest requires every decode wrapper name to be unique across all
// factschema_decode*.go seam files. Root's wrapper adapts the envelope
// through the same factenvelope.FactSchemaFromInternal call, and the sole
// caller here (awsResourceTypeForEnvelope) discards the error entirely, so
// this direct call is behavior-identical to the classified call for that
// check. It mirrors the ec2 family's independent decodeEC2InstancePosture
// copy, which the repo already keeps per-package rather than shared.
func decodeObservabilityAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.Resource{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

// awsResourceTypeForEnvelope returns the resource_type string from an
// aws_resource fact payload, or empty when absent or undecodable — an empty
// string never matches the observabilityResourceTypes set, so a malformed
// aws_resource fact is simply not a trigger rather than an error.
func awsResourceTypeForEnvelope(envelope facts.Envelope) string {
	resource, err := decodeObservabilityAWSResource(envelope)
	if err != nil {
		return ""
	}
	return resource.ResourceType
}
