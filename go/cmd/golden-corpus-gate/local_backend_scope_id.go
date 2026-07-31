// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// localBackendScopeIDSentinel is the placeholder value the B-12 snapshot uses
// in place of a literal scope_id for query shapes that exercise a
// BackendLocal state-snapshot scope (issue #5594's golden-corpus coverage).
//
// Every other backend kind's locator this gate has ever pinned (s3's
// "s3://bucket/key") is a pure string derived from the .tf file's own
// attribute values, with no filesystem dependency, so its ScopeLocatorHash
// is knowable when the snapshot is authored and can be a literal in
// testdata/golden/e2e-20repo-snapshot.json. A BackendLocal candidate's
// locator (go/internal/collector/terraformstate/backend_config_local.go) is
// instead an ABSOLUTE PATH built from the repository's real git-checkout
// root, and scripts/verify-golden-corpus-gate.sh runs every fixture repo
// inside a fresh `mktemp -d` working directory, so that absolute path (and
// therefore the hash) differs on every run. It cannot be a literal in the
// committed snapshot.
//
// The orchestrator resolves the real value after bootstrap-index commits the
// terraform_local_backend_demo fixture's repository fact (querying its
// local_path, then invoking this binary in -print-local-backend-scope-id
// mode to compute the same hash formula production code uses) and feeds it
// back via -local-backend-scope-id; substituteLocalBackendScopeID (below)
// replaces every occurrence of this sentinel in the loaded snapshot before
// the query phase runs.
const localBackendScopeIDSentinel = "$LOCAL_BACKEND_SCOPE_ID$"

// localBackendDefaultStatePath mirrors
// terraformstate.defaultTerraformLocalStatePath (Terraform's own default:
// https://developer.hashicorp.com/terraform/language/backend/local).
// Duplicated as a literal, not imported, because this CLI-only helper does
// not otherwise depend on the collector package for one constant and one
// hash call — see computeLocalBackendScopeID's doc comment for why the hash
// formula itself is also duplicated rather than imported.
const localBackendDefaultStatePath = "terraform.tfstate"

// computeLocalBackendScopeID reproduces, byte for byte, the join-key formula
// production code uses for a bare `backend "local" {}` block whose .tf file
// sits at the fixture repo's root (so the backend block's containing
// directory IS the repo root, and the module-dir join component is empty):
//
//	absoluteLocator = filepath.Join(repoLocalPath, "terraform.tfstate")
//	scopeID = "state_snapshot:local:" + sha256_hex("local\x00" + absoluteLocator)
//
// This mirrors terraformstate.ScopeLocatorHash(BackendLocal, absoluteLocator)
// (go/internal/collector/terraformstate/identity.go) composed with
// backendConfigLocalCandidate's directory join
// (go/internal/collector/terraformstate/backend_config_local.go) for the
// specific, deliberately simple case this fixture uses (root-level backend
// block, default path). The formula is duplicated here rather than imported
// because go/cmd/golden-corpus-gate intentionally has no dependency on
// go/internal/collector (an I/O-and-orchestration command binary pulling in
// a full collector package for one hash call is the wrong direction of
// coupling); TestComputeLocalBackendScopeIDMatchesScopeLocatorHash pins the
// two formulas against each other directly so a change to either one that
// breaks the join is caught locally, without a live gate run.
func computeLocalBackendScopeID(repoLocalPath string) (string, error) {
	if repoLocalPath == "" {
		return "", fmt.Errorf("repo local path must not be empty")
	}
	absoluteLocator := filepath.Join(repoLocalPath, localBackendDefaultStatePath)
	sum := sha256.Sum256([]byte("local\x00" + absoluteLocator))
	return "state_snapshot:local:" + hex.EncodeToString(sum[:]), nil
}

// substituteLocalBackendScopeID replaces every occurrence of
// localBackendScopeIDSentinel in the snapshot's HTTP and MCP query shapes
// (RequestBody, Arguments, and RequiredJSONValues) with the real, run-time
// computed scope_id. A no-op when scopeID is empty, so every invocation that
// does not pass -local-backend-scope-id (every existing focused local test,
// and any run of a snapshot with no sentinel) is unaffected.
func substituteLocalBackendScopeID(snap *Snapshot, scopeID string) {
	if scopeID == "" {
		return
	}
	for key, shape := range snap.QueryShapes.HTTP {
		substituteSentinelInShape(&shape, scopeID)
		snap.QueryShapes.HTTP[key] = shape
	}
	for key, shape := range snap.QueryShapes.MCP {
		substituteSentinelInShape(&shape, scopeID)
		snap.QueryShapes.MCP[key] = shape
	}
}

func substituteSentinelInShape(shape *QueryShape, scopeID string) {
	substituteSentinelInMap(shape.RequestBody, scopeID)
	substituteSentinelInMap(shape.Arguments, scopeID)
	substituteSentinelInMap(shape.RequiredJSONValues, scopeID)
}

func substituteSentinelInMap(m map[string]any, scopeID string) {
	for k, v := range m {
		if s, ok := v.(string); ok && s == localBackendScopeIDSentinel {
			m[k] = scopeID
		}
	}
}
