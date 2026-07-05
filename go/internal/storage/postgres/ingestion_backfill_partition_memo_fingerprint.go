// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// deferredCatalogFingerprint is a stable digest over the shared catalog-derived
// $1/$2 query parameters (buildDeferredScopedFactQueryParams) that every
// partition's deferred fact-load query is bound by. It is the partition memo
// gate's catalog-change signal (issue #3624 Track 1 / B'): any repository
// onboard, rename, or removal changes the $1 non-repo_id anchor terms or the $2
// repo_id values, which flips the fingerprint and invalidates every memoized
// partition for this pass, forcing a full reload. Without this signal, a memo
// keyed only on (scope_id, generation_id) would incorrectly skip a partition
// whose OWN facts are unchanged but whose evidence could now resolve (or stop
// resolving) against a DIFFERENT catalog shape — a correctness bug, not merely a
// missed optimization.
//
// The fingerprint is order-insensitive: it sorts both parameter arrays before
// hashing, so the same catalog produces the same fingerprint regardless of the
// non-deterministic map/slice iteration order buildDeferredScopedFactQueryParams
// or its callers may use upstream. It is a pure function of the two arrays, not
// of process state, so it is reproducible across pass runs and across processes
// (a requirement for comparing a freshly computed fingerprint against one
// persisted by an earlier pass).
func deferredCatalogFingerprint(params deferredScopedFactQueryParams) string {
	nonRepoIDLike := append([]string(nil), []string(params.nonRepoIDLike)...)
	repoIDValues := append([]string(nil), []string(params.repoIDValues)...)
	sort.Strings(nonRepoIDLike)
	sort.Strings(repoIDValues)

	var builder strings.Builder
	builder.WriteString("nonRepoIDLike:")
	for _, term := range nonRepoIDLike {
		builder.WriteString(term)
		builder.WriteByte('\x00')
	}
	builder.WriteString("repoIDValues:")
	for _, value := range repoIDValues {
		builder.WriteString(value)
		builder.WriteByte('\x00')
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
