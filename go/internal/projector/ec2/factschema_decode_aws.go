// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// This file holds this family's decode wrapper for the ec2_instance_posture
// fact kind, named factschema_decode_aws.go to match the repo-wide convention
// (root's go/internal/projector/factschema_decode_aws.go,
// go/internal/reducer/factschema_decode.go) so the payload-usage manifest gate
// (scripts/verify-payload-usage-manifest.sh, issue #4573) discovers it: that
// gate globs factschema_decode*.go files and AST-scans each function body for
// a factschema.FactKindXxx reference to recognize it as a decode seam.

// decodeEC2InstancePosture decodes one ec2_instance_posture envelope into the
// typed awsv1.EC2InstancePosture struct through the contracts seam. This
// family package keeps its own decode call rather than importing root
// projector's classified-decode wrapper (root's factschema_decode_aws.go):
// sharing it would require this package to import root, which root already
// imports to dispatch to this package — an import cycle. The sole caller
// (BuildUsesProfileMaterializationReducerIntent) only checks err != nil and
// discards the wrapped error's classification, so this direct
// factschema.DecodeEC2InstancePosture call plus fact-kind-labeled wrapping is
// behavior-identical to the classified call for that check. This mirrors
// go/internal/reducer's own independent decodeEC2InstancePosture copy
// (factschema_decode.go), which the repo already keeps per-package rather
// than shared.
func decodeEC2InstancePosture(env facts.Envelope) (awsv1.EC2InstancePosture, error) {
	posture, err := factschema.DecodeEC2InstancePosture(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return awsv1.EC2InstancePosture{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindEC2InstancePosture, err)
	}
	return posture, nil
}
