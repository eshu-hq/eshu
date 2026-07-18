// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ProductionCypherSHA256 returns the exact-text fingerprint stored by handler
// manifests. Whitespace is included so the manifest cannot silently describe a
// simplified or normalized query instead of the production-emitted shape.
func ProductionCypherSHA256(cypher string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cypher)))
}

// ProductionCypherFamilySHA256 fingerprints a complete named set of structural
// query variants. Variant names and exact query bytes are both load-bearing.
func ProductionCypherFamilySHA256(variants map[string]string) string {
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	sort.Strings(names)
	digest := sha256.New()
	for _, name := range names {
		_, _ = digest.Write([]byte(name))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(variants[name]))
		_, _ = digest.Write([]byte{0})
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

// BindProductionCypher replaces manifest metadata with the exact query strings
// emitted by production builders after verifying one-to-one IDs and hashes.
func BindProductionCypher(manifest Manifest, production map[string]string) (Manifest, error) {
	manifest.Entries = append([]Entry(nil), manifest.Entries...)
	seen := make(map[string]struct{}, len(manifest.Entries))
	var violations []string
	for index := range manifest.Entries {
		entry := &manifest.Entries[index]
		if entry.QueryKind != queryKindCypher {
			continue
		}
		seen[entry.ID] = struct{}{}
		if strings.TrimSpace(entry.Cypher) != "" {
			violations = append(violations, fmt.Sprintf("%s: manifest must not copy production Cypher", entry.ID))
			continue
		}
		cypher, ok := production[entry.ID]
		if !ok {
			violations = append(violations, fmt.Sprintf("%s: missing production Cypher", entry.ID))
			continue
		}
		if got := ProductionCypherSHA256(cypher); got != entry.CypherSHA256 {
			violations = append(violations, fmt.Sprintf("%s: production Cypher SHA-256 mismatch", entry.ID))
			continue
		}
		entry.Cypher = cypher
	}
	for id := range production {
		if _, ok := seen[id]; !ok {
			violations = append(violations, fmt.Sprintf("unregistered production Cypher %s", id))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return Manifest{}, errors.New(strings.Join(violations, "; "))
	}
	return manifest, nil
}
