// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// waiverDateLayout is the required format for a MaterializedEdgeWaiver.Waived
// date: ISO-8601 calendar date, matching the language-feature-parity ledger
// and other repo waiver-adjacent conventions.
const waiverDateLayout = "2006-01-02"

// materializedEdgeWaiverFile is the on-disk shape of the `waivers:` section
// this package's own coverage manifest (specs/ifa-materialized-edge-
// coverage.v1.yaml) carries alongside the standard replaycoverage `coverage:`
// rows. It is a deliberate, small addition on top of replaycoverage's generic
// Manifest schema (which has no issue-tracked-waiver concept — see
// materialized_edges.go's RunMaterializedEdgeCoverage doc comment) parsed
// from the SAME file replaycoverage.LoadManifest reads, picking out only the
// key replaycoverage.LoadManifest does not know about.
type materializedEdgeWaiverFile struct {
	Waivers []materializedEdgeWaiverFileEntry `yaml:"waivers"`
}

type materializedEdgeWaiverFileEntry struct {
	Surface string `yaml:"surface"`
	Issue   string `yaml:"issue"`
	Waived  string `yaml:"waived"`
	Reason  string `yaml:"reason"`
}

// LoadMaterializedEdgeWaivers reads the `waivers:` section of the
// materialized-edge coverage manifest at path. A missing file returns an
// empty waiver list (every family then reports uncovered-and-unwaived — the
// honest red state), not an error, mirroring replaycoverage.LoadManifest's
// own missing-file contract. Every waiver row must carry a non-blank surface,
// issue, and reason, a Waived date matching YYYY-MM-DD, and a surface unique
// across the file — any of those being wrong silently corrupts which
// families are honestly tracked, so this fails loudly instead.
func LoadMaterializedEdgeWaivers(path string) ([]MaterializedEdgeWaiver, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured coverage manifest under specs/, not external input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ifa: read materialized-edge coverage manifest %s: %w", path, err)
	}
	var parsed materializedEdgeWaiverFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse materialized-edge coverage manifest %s waivers: %w", path, err)
	}

	seen := make(map[string]struct{}, len(parsed.Waivers))
	out := make([]MaterializedEdgeWaiver, 0, len(parsed.Waivers))
	for _, entry := range parsed.Waivers {
		surface := strings.TrimSpace(entry.Surface)
		issue := strings.TrimSpace(entry.Issue)
		waived := strings.TrimSpace(entry.Waived)
		reason := strings.TrimSpace(entry.Reason)
		if surface == "" {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver has blank surface", path)
		}
		if issue == "" {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q has blank issue", path, surface)
		}
		if reason == "" {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q has blank reason", path, surface)
		}
		if _, err := time.Parse(waiverDateLayout, waived); err != nil {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q has invalid waived date %q (want YYYY-MM-DD): %w", path, surface, waived, err)
		}
		if _, dup := seen[surface]; dup {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q declared twice", path, surface)
		}
		seen[surface] = struct{}{}
		out = append(out, MaterializedEdgeWaiver{Surface: surface, Issue: issue, Waived: waived, Reason: reason})
	}
	return out, nil
}
