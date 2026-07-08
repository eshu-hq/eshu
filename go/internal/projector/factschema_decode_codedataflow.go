// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codedataflowv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codedataflow/v1"
)

func decodeCodeDataflowScanned(env facts.Envelope) (codedataflowv1.DataflowScanned, error) {
	scanned, err := factschema.DecodeCodeDataflowScanned(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.DataflowScanned{}, newProjectorDecodeError(factschema.FactKindCodeDataflowScanned, err)
	}
	return scanned, nil
}

func decodeCodeFunctionSummary(env facts.Envelope) (codedataflowv1.FunctionSummary, error) {
	summary, err := factschema.DecodeCodeFunctionSummary(factschemaEnvelope(env))
	if err != nil {
		return codedataflowv1.FunctionSummary{}, newProjectorDecodeError(factschema.FactKindCodeFunctionSummary, err)
	}
	return summary, nil
}
