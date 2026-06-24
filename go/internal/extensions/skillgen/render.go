// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RenderResult is the per-host output of a generation or check run.
type RenderResult struct {
	// Host is the host this result belongs to.
	Host Host
	// OutputPath is the host-relative path inside the configured expected/
	// root, e.g. ".claude/skills/eshu/SKILL.md".
	OutputPath string
	// Bytes is the rendered content.
	Bytes []byte
}

// RenderAll loads fragments, formats the byte-citation block, and renders
// every registered host. The output is deterministic; the same input
// always produces the same bytes.
//
// The function does not touch the filesystem; callers write the results
// to expected/ (gen) or byte-compare them against expected/ (check).
func RenderAll(fragments []Fragment, caps Capabilities) ([]RenderResult, error) {
	citations := make([]string, 0, len(fragments))
	for _, f := range fragments {
		normalized, err := NormalizeByteCitation(f.ByteCitation, f.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("fragment %s: %w", f.ID, err)
		}
		citations = append(citations, normalized)
	}
	commentBlock := FormatCommentBlock(citations)
	results := make([]RenderResult, 0, len(AllHosts()))
	for _, h := range AllHosts() {
		adapter, err := AdapterFor(h)
		if err != nil {
			return nil, err
		}
		out, err := adapter.Render(RenderInput{
			Fragments:    fragments,
			CommentBlock: commentBlock,
			Capabilities: caps,
		})
		if err != nil {
			return nil, fmt.Errorf("render host %s: %w", h, err)
		}
		results = append(results, RenderResult{
			Host:       h,
			OutputPath: adapter.OutputPath(),
			Bytes:      out,
		})
	}
	return results, nil
}

// WriteExpected writes a RenderResult set to disk under expectedRoot/<host>/<output_path>.
// The function is used by the `gen` subcommand and by tests that need to
// inspect the on-disk form of the generated output.
func WriteExpected(expectedRoot string, results []RenderResult) error {
	for _, r := range results {
		target := filepath.Join(expectedRoot, string(r.Host), r.OutputPath)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", target, err)
		}
		if err := os.WriteFile(target, r.Bytes, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return nil
}

// CheckDrift compares the in-memory RenderResult set against the on-disk
// expected/ baseline. It returns a slice of Drift findings, one per host
// whose bytes do not match the committed baseline. A nil slice means the
// baseline is in lockstep with the fragments.
type Drift struct {
	// Host is the host that drifted.
	Host Host
	// Path is the on-disk file that drifted.
	Path string
	// Reason classifies the drift; the byte-comparison reason is "content_mismatch".
	Reason string
}

func CheckDrift(expectedRoot string, results []RenderResult) ([]Drift, error) {
	var drifts []Drift
	for _, r := range results {
		path := filepath.Join(expectedRoot, string(r.Host), r.OutputPath)
		disk, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				drifts = append(drifts, Drift{Host: r.Host, Path: path, Reason: "missing"})
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if !bytes.Equal(disk, r.Bytes) {
			drifts = append(drifts, Drift{Host: r.Host, Path: path, Reason: "content_mismatch"})
		}
	}
	// Sort for deterministic output.
	sort.Slice(drifts, func(i, j int) bool { return drifts[i].Host < drifts[j].Host })
	return drifts, nil
}
