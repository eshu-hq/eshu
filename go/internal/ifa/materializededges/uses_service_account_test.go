// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// TestUsesServiceAccountCassetteDrivesTheExpectedEdgeSet proves the
// committed collector-only cassette replays through the production
// trust-chain build to the same SECRETS_IAM_USES_SERVICE_ACCOUNT exact set
// the vacuity guard reproduces from the compiled Odù.
//
// The cassette and the compiled Odù are deliberately different fixtures:
// the Odù carries reducer read-model facts (the projection handler's input),
// while the cassette carries the collector facts a live gate replays (the
// trust-chain handler's input). They meet only at the edge set: two exact
// chains (IRSA web-identity for the orders workload, EKS Pod Identity for
// the billing workload) each yield one workload -> service-account edge,
// while the fingerprint-only stale lane and the workload-less orphan lane
// resolve to nothing.
//
// A cassette that stopped decoding against its contracts, or that silently
// lost a fact, would understate the live proof the coverage row will one
// day attest -- so a quarantined fact, a wrong fact count, or a read-model
// fact smuggled into a collector cassette all fail here first.
func TestUsesServiceAccountCassetteDrivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()

	root := repoRootDir(t)
	loaded, err := ifa.LoadUsesServiceAccountFamilyOdu(ifa.UsesServiceAccountFamilyCassetteFullPath(root))
	if err != nil {
		t.Fatalf("LoadUsesServiceAccountFamilyOdu: %v", err)
	}
	if loaded.Name != ifa.UsesServiceAccountFamilyOduName {
		t.Fatalf("cassette projects Odù %q, want %q", loaded.Name, ifa.UsesServiceAccountFamilyOduName)
	}
	const wantFacts = 27
	if len(loaded.Facts) != wantFacts {
		t.Fatalf("cassette carries %d facts, want %d (two exact lanes, two non-producer lanes, two pod-template observations, two aws_resource observations)", len(loaded.Facts), wantFacts)
	}
	for i, env := range loaded.Facts {
		if strings.HasPrefix(env.FactKind, "reducer_") {
			t.Fatalf("cassette fact %d carries read-model kind %q: a cassette is recorded collector output, never reducer read models", i, env.FactKind)
		}
		if env.IsTombstone {
			t.Fatalf("cassette fact %d (kind %q) is a tombstone: retractions are reducer generations, not collector output", i, env.FactKind)
		}
	}

	rows, problem := runSecretsIAMFamilyChain(loaded)
	if problem != "" {
		t.Fatalf("runSecretsIAMFamilyChain over the cassette: %s", problem)
	}
	expected, _, problem := loadDirectFamilyExpectedEdges(
		usesServiceAccountExpectedEdgesPath(root), usesServiceAccountFamily, loaded.Name,
	)
	if problem != "" {
		t.Fatalf("loadDirectFamilyExpectedEdges: %s", problem)
	}
	actual := usesServiceAccountRowsToExpectedEdges(rows.UsesServiceAccountEdges)
	if mismatch := compareDirectFamilyExpectedEdges(loaded.Name, usesServiceAccountFamily, expected, actual); mismatch != "" {
		t.Fatalf("cassette chain %s", mismatch)
	}
}
