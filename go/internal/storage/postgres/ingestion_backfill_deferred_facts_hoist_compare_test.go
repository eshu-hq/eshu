// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// deferredHoistCatalog is the full catalog the differential proof discovers
// evidence against; it must include every repo_id referenced by the fixture
// above (including targets that never appear as a fact source themselves).
func deferredHoistCatalog() []relationships.CatalogEntry {
	return []relationships.CatalogEntry{
		{RepoID: "github.com/org/app", Aliases: []string{"github.com/org/app", "edge-app"}},
		{RepoID: "github.com/org/app-config", Aliases: []string{"github.com/org/app-config"}},
		{RepoID: "repo-vault", Aliases: []string{"repo-vault", "secret-store"}},
		{RepoID: "repo-gitops", Aliases: []string{"repo-gitops", "gitops-controller"}},
		{RepoID: "repo-payments", Aliases: []string{"repo-payments", "payments-service"}},
		{RepoID: "repo-gcp-source", Aliases: []string{"repo-gcp-source", "order-gateway"}},
		{RepoID: "deploy-toolkit", Aliases: []string{"deploy-toolkit"}},
		{RepoID: "github.com/org/.github", Aliases: []string{"github.com/org/.github"}},
		{RepoID: "repo-noise-target", Aliases: []string{"repo-noise-target"}},
	}
}

func evidenceSetsEqual(a, b []relationships.EvidenceFact) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(f relationships.EvidenceFact) string {
		return string(f.EvidenceKind) + "|" + f.SourceRepoID + "|" + f.TargetRepoID + "|" + fmt.Sprint(f.Details["path"]) + "|" + fmt.Sprint(f.Details["matched_value"])
	}
	seenA := make(map[string]int, len(a))
	for _, f := range a {
		seenA[key(f)]++
	}
	for _, f := range b {
		k := key(f)
		if seenA[k] == 0 {
			return false
		}
		seenA[k]--
	}
	for _, count := range seenA {
		if count != 0 {
			return false
		}
	}
	return true
}
