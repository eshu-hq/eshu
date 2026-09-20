// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perform

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// This file holds this family's decode wrappers for the aws_iam_permission and
// aws_resource_policy_permission fact kinds, named factschema_decode_iam.go to
// match the repo-wide convention (root's
// go/internal/projector/factschema_decode_aws.go,
// go/internal/projector/aws/ec2/factschema_decode_aws.go,
// go/internal/projector/cloud/aws/iam/trust/factschema_decode_iam.go,
// go/internal/reducer/schemadecode/factschema_decode.go) so the payload-usage
// manifest gate (scripts/verify-payload-usage-manifest.sh, issue #4573)
// discovers it: that gate globs factschema_decode*.go files under
// go/internal/projector and AST-scans each function body for a
// factschema.FactKindXxx reference to recognize it as a decode seam.

// decodeIAMCanPerformAWSIAMPermission decodes one aws_iam_permission envelope
// into the typed iamv1.Permission struct through the contracts seam. It is
// named distinctly from the sibling trust package's decodeAWSIAMPermission
// (rather than reusing that name) because
// scripts/verify-payload-usage-manifest.sh requires every decode<Kind> wrapper
// under go/internal/projector to have a globally unique function name — two
// packages independently decoding the same fact kind is expected (each family
// owns its own trigger predicate), but the manifest gate keys seams by bare
// identifier, not by (package, identifier). This family package keeps its own
// decode call rather than importing the sibling trust package's decode
// function or root projector's classified decode wrapper: sharing either would
// require this package to import a package root already imports to dispatch to
// it, which cycles. The sole caller
// (BuildIAMCanPerformMaterializationReducerIntent) only checks err != nil and
// discards the wrapped error's classification, so this direct
// factschema.DecodeAWSIAMPermission call plus fact-kind-labeled wrapping is
// behavior-identical to the classified call for that check. This mirrors
// go/internal/projector/aws/ec2's decodeEC2InstancePosture and
// go/internal/projector/cloud/aws/iam/trust's own independent
// decodeAWSIAMPermission copy, which the repo already keeps per-package rather
// than shared.
func decodeIAMCanPerformAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	permission, err := factschema.DecodeAWSIAMPermission(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return iamv1.Permission{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSIAMPermission, err)
	}
	return permission, nil
}

// decodeIAMCanPerformAWSResourcePolicyPermission decodes one
// aws_resource_policy_permission envelope into the typed
// iamv1.ResourcePolicyPermission struct through the contracts seam. See
// decodeIAMCanPerformAWSIAMPermission above for the naming rationale and for
// why this package keeps its own decode call rather than sharing one.
func decodeIAMCanPerformAWSResourcePolicyPermission(env facts.Envelope) (iamv1.ResourcePolicyPermission, error) {
	permission, err := factschema.DecodeAWSResourcePolicyPermission(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return iamv1.ResourcePolicyPermission{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindAWSResourcePolicyPermission, err)
	}
	return permission, nil
}
