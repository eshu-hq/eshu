// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// Supply-chain impact probe doubles shared by root internal/query tests and
// the supplychain handler-family tests. They lived as twin declarations in
// both places (root's cloud-runtime parity test and the hub probe tests),
// held identical by prose; a twin that drifts passes while guarding nothing,
// so the single copy lives here.
//
// Only doubles whose signatures name leaf packages (impact, querycontract)
// can live here. Doubles over handler-family types (the hub's
// CloudResourceRuntimeDigestMatch, KubernetesRuntimeCandidate,
// KubernetesRuntimeWorkloadMatch) stay declared per test file: importing the
// family from here is the cycle doc.go forbids. See AGENTS.md.

// FakeCloudRuntimeGraph is a GraphQuery stub for the runtime-image probe. It
// returns RowsByDigest for any digest present in the query params and records
// the digest list the probe passed, so a test can assert both the promotion
// outcome and that the probe bounded/deduplicated its input.
type FakeCloudRuntimeGraph struct {
	RowsByDigest map[string][]map[string]any
	Err          error
	GotDigests   []string
}

// Run answers a multi-digest read from RowsByDigest, recording the requested
// digests, or fails with Err.
func (s *FakeCloudRuntimeGraph) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	digests, _ := params["digests"].([]string)
	s.GotDigests = append([]string(nil), digests...)
	var rows []map[string]any
	for _, digest := range digests {
		rows = append(rows, s.RowsByDigest[digest]...)
	}
	return rows, nil
}

// RunSingle answers a single-row read with no rows; the probe never uses it.
func (s *FakeCloudRuntimeGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// KubernetesRuntimeCall records one per-digest probe query a graph double
// observed, so a test can assert the probe planned one query per digest.
type KubernetesRuntimeCall struct {
	Digest string
	Limit  int
}

// FakeKubernetesRuntimeGraph is a GraphQuery stub for the Kubernetes runtime
// probe. It filters Rows to the single requested subject digest, enforces the
// caller limit, and records the cypher, params, and per-digest calls.
type FakeKubernetesRuntimeGraph struct {
	mu       sync.Mutex
	Rows     []map[string]any
	Cypher   string
	Params   map[string]any
	RunCalls int
	Calls    []KubernetesRuntimeCall
	Err      error
}

// Run answers a single-subject-digest read, recording the call, or fails
// with Err. A multi-digest call is a planner violation and fails with Err,
// which is nil in the happy path so the zero value still exercises the
// violation branch.
func (s *FakeKubernetesRuntimeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RunCalls++
	s.Cypher = cypher
	s.Params = params
	digests, _ := params["subject_digests"].([]string)
	if len(digests) == 1 {
		s.Calls = append(s.Calls, KubernetesRuntimeCall{Digest: digests[0], Limit: querycontract.IntVal(params, "limit")})
	}
	if s.Err != nil || len(digests) != 1 {
		return nil, s.Err
	}
	filtered := make([]map[string]any, 0, len(s.Rows))
	for _, row := range s.Rows {
		if querycontract.StringVal(row, "matched_digest") == digests[0] {
			filtered = append(filtered, row)
		}
	}
	if limit := querycontract.IntVal(params, "limit"); len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// RunSingle answers a single-row read with no rows; the probe never uses it.
func (s *FakeKubernetesRuntimeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// Snapshot returns the total Run count and the recorded per-digest calls.
func (s *FakeKubernetesRuntimeGraph) Snapshot() (int, []KubernetesRuntimeCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RunCalls, append([]KubernetesRuntimeCall(nil), s.Calls...)
}

// FakeRuntimeContextFindingStore is a finding-store stub for the runtime
// context probe. It answers the three reads from in-memory rows and records
// the selectors each read received.
type FakeRuntimeContextFindingStore struct {
	Rows            []impact.SupplyChainImpactFindingRow
	ByRepo          map[string]impact.SupplyChainRuntimeContext
	ByDigest        map[string]map[string]string
	Called          []string
	EnvCandidates   []impact.SupplyChainRuntimeEnvironmentCandidate
	AllowedRepoIDs  []string
	AllowedScopeIDs []string
	Err             error
}

// ListSupplyChainImpactRuntimeEnvironmentEvidence answers from ByDigest,
// recording the candidates and grants, or fails with Err.
func (f *FakeRuntimeContextFindingStore) ListSupplyChainImpactRuntimeEnvironmentEvidence(
	_ context.Context,
	candidates []impact.SupplyChainRuntimeEnvironmentCandidate,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]map[string]string, error) {
	f.EnvCandidates = append([]impact.SupplyChainRuntimeEnvironmentCandidate(nil), candidates...)
	f.AllowedRepoIDs = append([]string(nil), allowedRepositoryIDs...)
	f.AllowedScopeIDs = append([]string(nil), allowedScopeIDs...)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ByDigest, nil
}

// ListSupplyChainImpactFindings answers from Rows.
func (f *FakeRuntimeContextFindingStore) ListSupplyChainImpactFindings(
	context.Context,
	impact.SupplyChainImpactFindingFilter,
) ([]impact.SupplyChainImpactFindingRow, error) {
	return append([]impact.SupplyChainImpactFindingRow(nil), f.Rows...), nil
}

// ListSupplyChainImpactRuntimeContext answers from ByRepo, recording the
// selectors and grants, or fails with Err.
func (f *FakeRuntimeContextFindingStore) ListSupplyChainImpactRuntimeContext(
	_ context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]impact.SupplyChainRuntimeContext, error) {
	f.Called = append([]string(nil), repositoryIDs...)
	f.AllowedRepoIDs = append([]string(nil), allowedRepositoryIDs...)
	f.AllowedScopeIDs = append([]string(nil), allowedScopeIDs...)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ByRepo, nil
}
