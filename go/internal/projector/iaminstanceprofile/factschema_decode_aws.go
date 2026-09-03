// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstanceprofile

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds this family's decode wrapper for the aws_resource fact
// kind, named factschema_decode_aws.go to match the repo-wide convention
// (go/internal/projector/ec2/factschema_decode_aws.go,
// go/internal/projector/observabilitycoverage/factschema_decode_aws.go) so
// the payload-usage manifest gate (scripts/verify-payload-usage-manifest.sh,
// issue #4573) discovers it: that gate globs factschema_decode*.go files
// recursively and AST-scans each function body for a factschema.FactKindXxx
// reference to recognize it as a decode seam.

// decodeIAMInstanceProfileAWSResource decodes one aws_resource envelope into
// the typed awsv1.Resource struct through the contracts seam. This family
// package keeps its own decode call rather than importing root projector's
// classified-decode wrapper (root's decodeAWSResource, since removed with the
// observability-coverage-materialization extraction that was its last
// caller): sharing it would have required this package to import root, which
// root already imports to dispatch to this package — an import cycle. The
// name carries the family prefix because the payload-usage manifest requires
// every decode wrapper name to be unique across all factschema_decode*.go
// seam files. Root's wrapper adapted the envelope through the same
// factenvelope.FactSchemaFromInternal call (via its factschemaEnvelope
// alias), and the sole caller here — the trigger predicate in
// BuildIAMInstanceProfileRoleMaterializationReducerIntent — checks only
// err != nil and discards the wrapped error's classification, so this direct
// call is behavior-identical to the classified call for that check. It
// mirrors the ec2 family's independent decodeEC2InstancePosture copy, which
// the repo already keeps per-package rather than shared.
func decodeIAMInstanceProfileAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.Resource{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSResource, err)
	}
	return resource, nil
}
