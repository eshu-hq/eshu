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
// partition's deferred fact-load query is bound by, plus the catalog's
// normalized RemoteURLs (issue #5483 C2). It is the partition memo gate's
// catalog-change signal (issue #3624 Track 1 / B'): any repository onboard,
// rename, or removal changes the $1 non-repo_id anchor terms or the $2 repo_id
// values, and any repository remote_url change moves the RemoteURL set, each of
// which flips the fingerprint and invalidates every memoized partition for this
// pass, forcing a full reload. Without this signal, a memo keyed only on
// (scope_id, generation_id) would incorrectly skip a partition whose OWN facts
// are unchanged but whose evidence could now resolve (or stop resolving)
// against a DIFFERENT catalog shape — a correctness bug, not merely a missed
// optimization.
//
// RemoteURL is a fingerprint input even though it is NOT a query bind because
// the strict cross-repo Flux resolver (discoverStructuredFluxEvidence) matches
// a manifest's spec.url against each catalog repository's normalized RemoteURL
// by exact equality. A repository whose remote_url changes but whose name/slug
// aliases and repo_id do not (a mirror migration) would otherwise leave the
// fingerprint unchanged and memo-skip the Flux manifest partition, so the
// DEPLOYS_FROM edge would never re-resolve — the source-before-target /
// remote_url-change under-linking gap.
//
// The fingerprint is order-insensitive: it sorts every parameter array before
// hashing, so the same catalog produces the same fingerprint regardless of the
// non-deterministic map/slice iteration order buildDeferredScopedFactQueryParams
// or its callers may use upstream. It is a pure function of the arrays, not of
// process state, so it is reproducible across pass runs and across processes (a
// requirement for comparing a freshly computed fingerprint against one
// persisted by an earlier pass).
func deferredCatalogFingerprint(params deferredScopedFactQueryParams) string {
	nonRepoIDLike := append([]string(nil), []string(params.nonRepoIDLike)...)
	repoIDValues := append([]string(nil), []string(params.repoIDValues)...)
	remoteURLs := append([]string(nil), []string(params.remoteURLs)...)
	sort.Strings(nonRepoIDLike)
	sort.Strings(repoIDValues)
	sort.Strings(remoteURLs)

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
	builder.WriteString("remoteURLs:")
	for _, value := range remoteURLs {
		builder.WriteString(value)
		builder.WriteByte('\x00')
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
