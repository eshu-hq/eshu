// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcanassume

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// This file holds this family's decode wrapper for the aws_iam_permission fact
// kind, named factschema_decode_iam.go to match the repo-wide convention
// (root's go/internal/projector/factschema_decode_aws.go,
// go/internal/projector/ec2/factschema_decode_aws.go,
// go/internal/reducer/factschema_decode.go) so the payload-usage manifest gate
// (scripts/verify-payload-usage-manifest.sh, issue #4573) discovers it: that
// gate globs factschema_decode*.go files under go/internal/projector and
// AST-scans each function body for a factschema.FactKindXxx reference to
// recognize it as a decode seam. It moved here from root together with its
// only caller.

// decodeAWSIAMPermission decodes one aws_iam_permission envelope into the typed
// iamv1.Permission struct through the contracts seam. This family package
// keeps its own decode call rather than importing root projector's classified
// decode wrapper (newProjectorDecodeError in root's factschema_quarantine.go):
// sharing it would require this package to import root, which root already
// imports to dispatch to this package — an import cycle. The sole caller
// (BuildIAMCanAssumeMaterializationReducerIntent) only checks err != nil and
// discards the wrapped error's classification, so this direct
// factschema.DecodeAWSIAMPermission call plus fact-kind-labeled wrapping is
// behavior-identical to the classified call for that check. This mirrors
// go/internal/projector/ec2's decodeEC2InstancePosture and go/internal/reducer's
// own independent decodeAWSIAMPermission copy, which the repo already keeps
// per-package rather than shared.
func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	permission, err := factschema.DecodeAWSIAMPermission(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return iamv1.Permission{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSIAMPermission, err)
	}
	return permission, nil
}
