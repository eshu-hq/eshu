// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimageidentity

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds this family's decode wrapper for the aws_relationship fact
// kind, named factschema_decode_aws.go to match the repo-wide convention
// (go/internal/projector/ec2/factschema_decode_aws.go,
// go/internal/projector/observabilitycoverage/factschema_decode_aws.go) so
// the payload-usage manifest gate (scripts/verify-payload-usage-manifest.sh,
// issue #4573) discovers it: that gate globs factschema_decode*.go files
// recursively and AST-scans each function body for a factschema.FactKindXxx
// reference to recognize it as a decode seam.

// decodeContainerImageIdentityAWSRelationship decodes one aws_relationship
// envelope into the typed awsv1.Relationship struct through the contracts
// seam. Root's own decodeAWSRelationship wrapper (formerly in its
// factschema_decode_aws.go) had this package's trigger as its only caller,
// so it was removed rather than kept as dead code — the way root's
// decodeAWSIAMPermission wrapper moved out entirely when iamcanassume/ was
// extracted. This package recreates the call under a family-prefixed name
// (matching the family-prefixed naming the ec2 and observabilitycoverage
// extractions use; the payload-usage manifest gate does enforce a unique
// function name per decode-seam file set, though root's wrapper was deleted
// in this same commit so the plain name would not collide today) rather than
// importing a root wrapper, because root imports this
// package to dispatch to it and the reverse direction would cycle. The sole
// caller here (awsRelationshipTargetsContainerImage) discards the error
// entirely, so this direct call is behavior-identical to the classified
// call root used to make. It mirrors the ec2 and observabilitycoverage
// families' independent per-package decode copies for their own AWS
// fact kinds.
func decodeContainerImageIdentityAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.Relationship{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}

// awsRelationshipTargetsContainerImage reports whether an aws_relationship
// fact's TargetType names a container image. TargetType is optional
// (*string): an absent value and an undecodable envelope both report false,
// matching the pre-extraction root behavior of comparing the dereferenced
// (possibly empty) string against "container_image".
func awsRelationshipTargetsContainerImage(envelope facts.Envelope) bool {
	relationship, err := decodeContainerImageIdentityAWSRelationship(envelope)
	if err != nil {
		return false
	}
	if relationship.TargetType == nil {
		return false
	}
	return *relationship.TargetType == "container_image"
}
