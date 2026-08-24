// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
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
	Surface   string `yaml:"surface"`
	ProofGate string `yaml:"proof_gate"`
	Issue     string `yaml:"issue"`
	Waived    string `yaml:"waived"`
	Reason    string `yaml:"reason"`
}

// LoadMaterializedEdgeWaivers reads the `waivers:` section of the
// materialized-edge coverage manifest at path. A missing file returns an
// empty waiver list (every family then reports uncovered-and-unwaived — the
// honest red state), not an error, mirroring replaycoverage.LoadManifest's
// own missing-file contract. Every waiver row must carry a non-blank surface,
// proof_gate, issue, and reason, a Waived date matching YYYY-MM-DD, and a
// (surface, proof_gate) pair unique across the file. proof_gate must name one
// of the two gates the required scenario-type pair maps to
// (validMaterializedEdgeWaiverProofGates): a waiver is per-(surface, proof_gate)
// so it softens exactly one reconciled row, never a whole family. Any of those
// being wrong silently corrupts which rows are honestly tracked, so this fails
// loudly instead.
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

	seen := make(map[materializedEdgeWaiverKey]struct{}, len(parsed.Waivers))
	out := make([]MaterializedEdgeWaiver, 0, len(parsed.Waivers))
	for _, entry := range parsed.Waivers {
		surface := strings.TrimSpace(entry.Surface)
		proofGate := strings.TrimSpace(entry.ProofGate)
		issue := strings.TrimSpace(entry.Issue)
		waived := strings.TrimSpace(entry.Waived)
		reason := strings.TrimSpace(entry.Reason)
		if surface == "" {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver has blank surface", path)
		}
		if proofGate == "" {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q has blank proof_gate (waivers are per-(surface, proof_gate); a per-family waiver is too coarse)", path, surface)
		}
		if _, ok := validMaterializedEdgeWaiverProofGates[proofGate]; !ok {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver %q names unknown proof_gate %q (want one of %s or %s)", path, surface, proofGate, materializedEdgeProofGateBaseline, materializedEdgeProofGateFault)
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
		key := materializedEdgeWaiverKey{Surface: surface, ProofGate: proofGate}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("ifa: materialized-edge coverage manifest %s: waiver (%q, %q) declared twice", path, surface, proofGate)
		}
		seen[key] = struct{}{}
		out = append(out, MaterializedEdgeWaiver{Surface: surface, ProofGate: proofGate, Issue: issue, Waived: waived, Reason: reason})
	}
	return out, nil
}

// LoadMaterializedEdgeLedger reads BOTH halves of the materialized-edge claims
// ledger from specsDir and returns their merged coverage rows and waivers.
//
// The ledger is split across two files because the reducer's materialized-edge
// surface is two surfaces (#6181). MaterializedEdgeManifestFileName holds the
// shared-projection families that reach the graph through the ordering-safe
// intent path; MaterializedEdgeDirectManifestFileName holds the direct
// families, each written by its own port straight to a storage/cypher writer,
// whose coverage is tracked by #6228.
//
// Reading only one of them is the failure mode this whole gate exists to
// prevent: a family whose ledger file is never opened produces no row, no
// finding and no output, and the gate reports green without knowing it exists.
// So every caller reconciling the whole surface, and every rule asserted over
// all waivers, MUST come through here rather than calling
// LoadMaterializedEdgeWaivers on one path.
//
// A caller reconciling ONE half deliberately is the only exception, and it must
// pass that half's families alongside: the two shared-half fixtures in
// materialized_edges_falsegreen_test.go and
// materialized_edges_waiver_granularity_test.go run against
// reducer.MaterializedEdgeFamilies(), which the direct half's waivers do not
// name. Both say so at the call site.
//
// A (surface, proof_gate) waiver declared in BOTH files is rejected. Within one
// file LoadMaterializedEdgeWaivers already rejects a duplicate; across the two
// nothing would, and the pair would silently double-count in every waiver total
// a maintainer reads.
func LoadMaterializedEdgeLedger(specsDir string) (replaycoverage.Manifest, []MaterializedEdgeWaiver, error) {
	shared := filepath.Join(specsDir, MaterializedEdgeManifestFileName)
	direct := filepath.Join(specsDir, MaterializedEdgeDirectManifestFileName)

	manifest, err := replaycoverage.LoadManifest(shared)
	if err != nil {
		return replaycoverage.Manifest{}, nil, fmt.Errorf("ifa: load shared materialized-edge ledger %s: %w", shared, err)
	}
	directManifest, err := replaycoverage.LoadManifest(direct)
	if err != nil {
		return replaycoverage.Manifest{}, nil, fmt.Errorf("ifa: load direct materialized-edge ledger %s: %w", direct, err)
	}
	manifest.Coverage = append(manifest.Coverage, directManifest.Coverage...)

	waivers, err := LoadMaterializedEdgeWaivers(shared)
	if err != nil {
		return replaycoverage.Manifest{}, nil, err
	}
	directWaivers, err := LoadMaterializedEdgeWaivers(direct)
	if err != nil {
		return replaycoverage.Manifest{}, nil, err
	}

	seen := make(map[materializedEdgeWaiverKey]struct{}, len(waivers))
	for _, w := range waivers {
		seen[materializedEdgeWaiverKey{Surface: w.Surface, ProofGate: w.ProofGate}] = struct{}{}
	}
	for _, w := range directWaivers {
		key := materializedEdgeWaiverKey{Surface: w.Surface, ProofGate: w.ProofGate}
		if _, dup := seen[key]; dup {
			return replaycoverage.Manifest{}, nil, fmt.Errorf(
				"ifa: waiver (%q, %q) is declared in both %s and %s; a waiver belongs to exactly one ledger half",
				w.Surface, w.ProofGate, MaterializedEdgeManifestFileName, MaterializedEdgeDirectManifestFileName)
		}
		seen[key] = struct{}{}
	}
	return manifest, append(waivers, directWaivers...), nil
}
