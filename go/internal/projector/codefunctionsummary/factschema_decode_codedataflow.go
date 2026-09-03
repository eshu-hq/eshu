// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codefunctionsummary

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
)

// This file holds this family's decode wrappers for the code_function_summary
// and code_dataflow_scanned fact kinds, named factschema_decode_codedataflow.go
// to match the repo-wide convention (root's now-removed
// go/internal/projector/factschema_decode_codedataflow.go, before this
// extraction) so the payload-usage manifest gate
// (scripts/verify-payload-usage-manifest.sh, issue #4573) discovers it: that
// gate globs factschema_decode*.go files and AST-scans each function body for
// a factschema.FactKindXxx reference to recognize it as a decode seam.

// decodeCodeFunctionSummary decodes one code_function_summary envelope into
// the typed codedataflowv1.FunctionSummary struct through the contracts seam.
// This family package keeps its own decode call rather than importing root
// projector's wrapper: sharing it would require this package to import root,
// which root already imports to dispatch to this package -- an import cycle.
// Unlike the aws_resource decode siblings (ec2, iaminstanceprofile, s3) that
// leave a shared root copy behind because root retains other callers for that
// fact kind, root's decodeCodeFunctionSummary and decodeCodeDataflowScanned
// had exactly one caller each -- codeFunctionSummaryTriggerRepoID, the sole
// consumer moved here -- so root's copy moved with it instead of staying
// behind as dead code.
func decodeCodeFunctionSummary(env facts.Envelope) (codedataflowv1.FunctionSummary, error) {
	summary, err := factschema.DecodeCodeFunctionSummary(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return codedataflowv1.FunctionSummary{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindCodeFunctionSummary, err)
	}
	return summary, nil
}

// decodeCodeDataflowScanned decodes one code_dataflow_scanned envelope into
// the typed codedataflowv1.DataflowScanned struct through the contracts seam.
// See decodeCodeFunctionSummary for why this family keeps its own copy
// instead of importing root's decode wrapper.
func decodeCodeDataflowScanned(env facts.Envelope) (codedataflowv1.DataflowScanned, error) {
	scanned, err := factschema.DecodeCodeDataflowScanned(factenvelope.FactSchemaFromInternal(env))
	if err != nil {
		return codedataflowv1.DataflowScanned{}, fmt.Errorf("decode %s payload: %w", factschema.FactKindCodeDataflowScanned, err)
	}
	return scanned, nil
}
