// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds this family's decode wrapper for the s3_bucket_posture fact
// kind, named factschema_decode_aws.go to match the repo-wide convention
// (go/internal/projector/ec2/factschema_decode_aws.go,
// go/internal/reducer/factschema_decode.go) so the payload-usage manifest gate
// (scripts/verify-payload-usage-manifest.sh, issue #4573) discovers it: that
// gate globs factschema_decode*.go files and AST-scans each function body for
// a factschema.FactKindXxx reference to recognize it as a decode seam.

// decodeS3BucketPosture decodes one s3_bucket_posture envelope into the typed
// awsv1.S3BucketPosture struct through the contracts seam. This family package
// keeps its own decode call rather than importing root projector's classified-
// decode wrapper (newProjectorDecodeError in root's factschema_quarantine.go):
// sharing it would require this package to import root, which root already
// imports to dispatch to this package — an import cycle. The sole caller
// (BuildLogsToMaterializationReducerIntent) only checks err != nil and
// discards the wrapped error's classification, so this direct
// factschema.DecodeS3BucketPosture call plus fact-kind-labeled wrapping is
// behavior-identical to the classified call for that check. This mirrors the
// ec2 child package's own independent decodeEC2InstancePosture copy.
func decodeS3BucketPosture(env facts.Envelope) (awsv1.S3BucketPosture, error) {
	posture, err := factschema.DecodeS3BucketPosture(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.S3BucketPosture{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindS3BucketPosture, err)
	}
	return posture, nil
}
