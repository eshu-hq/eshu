// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// externalSearchAdmissionStub is the satellite-side shape: an external
// package that names only exported identifiers. It proves the supplemental
// pin is callable from outside package iac with a non-zero kind — a pin the
// satellite cannot call is void on arrival.
type externalSearchAdmissionStub struct {
	lastSearch iac.IaCInventorySearch
	rows       []iac.InventoryCandidate
}

func (s *externalSearchAdmissionStub) SearchActive(
	_ context.Context,
	search iac.IaCInventorySearch,
	_ querycontract.RepositoryAccessFilter,
) ([]iac.InventoryCandidate, error) {
	s.lastSearch = search
	return append([]iac.InventoryCandidate(nil), s.rows...), nil
}

var _ iac.IaCInventorySearchAdmission = (*externalSearchAdmissionStub)(nil)

// TestExternalPackageCanCallInventorySearchAdmission proves the exported kind
// seam is sufficient: an external package builds a non-zero search from
// exported constants and serves the pin without touching internal identifiers.
func TestExternalPackageCanCallInventorySearchAdmission(t *testing.T) {
	t.Parallel()

	stub := &externalSearchAdmissionStub{
		rows: []iac.InventoryCandidate{{ID: "content-entity:e_1", Name: "aws_s3_bucket.logs", GenerationID: "generation-active"}},
	}
	var admission iac.IaCInventorySearchAdmission = stub
	got, err := admission.SearchActive(t.Context(), iac.IaCInventorySearch{
		Kind:  iac.IaCInventorySearchKindResource,
		Query: "logging",
		Limit: 1,
	}, querycontract.RepositoryAccessFilter{})
	if err != nil {
		t.Fatalf("external SearchActive: %v", err)
	}
	if len(got) != 1 || got[0].ID != "content-entity:e_1" {
		t.Fatalf("external SearchActive = %#v, want the stub row", got)
	}
	if stub.lastSearch.Kind != iac.IaCInventorySearchKindResource {
		t.Fatalf("external search kind = %q, want %q", stub.lastSearch.Kind, iac.IaCInventorySearchKindResource)
	}
}
