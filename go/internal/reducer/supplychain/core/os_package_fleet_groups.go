// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osPackageFleetGroupKey identifies one group of digest-less installed rows
// (#9984): rows no sibling analysis resolves to an image digest, on the same
// release line at the same installed version.
type osPackageFleetGroupKey struct {
	line    string
	version string
}

// osPackageFleetLine returns the group's release line for a row. A row the
// host-line table can read uses that line, so "12", "12 (bookworm)" and "12.5"
// share debian:12. A row it cannot read (a distro with no table row, such as
// the rpm families, or a non-numeric version such as "edge") keeps its own
// distro and distro_version: nothing proves two such values are one line, so
// they stay apart. The prefix cannot collide with a table line, whose vendor is
// always a distro name.
func osPackageFleetLine(row supplychainmodel.OSPackage) string {
	if line := osHostReleaseLine(row.Distro, row.DistroVersion); line != "" {
		return line
	}
	return "unread:" + row.Distro + ":" + strings.TrimSpace(row.DistroVersion)
}

// splitOSPackagesByReleaseLine partitions the digest-less base into one index
// shard per (release line, installed version), so each host line gets its own
// finding and its own verdict (#9984). A shard keeps only the least row of each
// lookup key: this path has no SBOM component, so the classifier reads one row
// per shard through firstOSPackageImpactPath, and keeping the least makes that
// row, and the finding, independent of arrival order. The shards come back
// ordered by key, because coalesce keeps the first of two findings that share a
// writer identity. The input is left as it was.
func splitOSPackagesByReleaseLine(
	base map[string][]supplychainmodel.OSPackage,
) []map[string][]supplychainmodel.OSPackage {
	shards := make(map[osPackageFleetGroupKey]map[string][]supplychainmodel.OSPackage)
	// A fleet repeats a few distro versions across many rows, and reading a line
	// allocates, so each distinct pair is read once.
	lines := make(map[[2]string]string)
	for lookup, rows := range base {
		for _, row := range rows {
			distro := [2]string{row.Distro, row.DistroVersion}
			line, known := lines[distro]
			if !known {
				line = osPackageFleetLine(row)
				lines[distro] = line
			}
			key := osPackageFleetGroupKey{line: line, version: strings.TrimSpace(row.InstalledVersion)}
			shard := shards[key]
			if shard == nil {
				shard = make(map[string][]supplychainmodel.OSPackage, 1)
				shards[key] = shard
			}
			kept := shard[lookup]
			switch {
			case len(kept) == 0:
				shard[lookup] = []supplychainmodel.OSPackage{row}
			case compareOSPackageRows(row, kept[0]) < 0:
				kept[0] = row
			}
		}
	}
	keys := make([]osPackageFleetGroupKey, 0, len(shards))
	for key := range shards {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b osPackageFleetGroupKey) int {
		return cmp.Or(cmp.Compare(a.line, b.line), cmp.Compare(a.version, b.version))
	})
	out := make([]map[string][]supplychainmodel.OSPackage, 0, len(keys))
	for _, key := range keys {
		out = append(out, shards[key])
	}
	return out
}

// compareOSPackageRows is the one stable order over installed rows. It ends on
// the fact ID, which identifies a row, so only true duplicates compare equal.
func compareOSPackageRows(a, b supplychainmodel.OSPackage) int {
	return cmp.Or(
		cmp.Compare(a.InstalledVersion, b.InstalledVersion),
		cmp.Compare(a.PURL, b.PURL),
		cmp.Compare(a.ScopeID, b.ScopeID),
		cmp.Compare(a.GenerationID, b.GenerationID),
		cmp.Compare(a.FactID, b.FactID),
	)
}
