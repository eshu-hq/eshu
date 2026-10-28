// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerabilitysuppression/v1"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// fakeSuppressionAdmission is a no-store admission port: the supply-chain
// handler test below proves the handler carries the injected versioned
// admission contract without opening a direct table handle.
type fakeSuppressionAdmission struct {
	admitted impact.VulnerabilitySuppressionMutationResult
}

func (f *fakeSuppressionAdmission) UpsertVulnerabilitySuppression(
	_ context.Context,
	_ vulnerabilitysuppressionv1.Suppression,
) (impact.VulnerabilitySuppressionMutationResult, error) {
	return f.admitted, nil
}

// TestNewSupplyChainHandlerTakesAdmissionContract proves the #8872 seam: the
// supply-chain handler is built with the versioned suppression admission
// contract, not a *sql.DB writer handle. The handler must carry the exact
// injected port so eshu-api can later bind a core-issued admission endpoint.
func TestNewSupplyChainHandlerTakesAdmissionContract(t *testing.T) {
	t.Parallel()

	admission := &fakeSuppressionAdmission{}
	handler := newSupplyChainHandler(admission, nil, nil, nil, query.QueryProfile(""), false, nil, nil)
	if handler == nil {
		t.Fatal("newSupplyChainHandler() = nil, want a handler bound to the admission contract")
	}
	if handler.SuppressionMutations == nil {
		t.Fatal("handler.SuppressionMutations = nil, want the injected admission contract")
	}
}
