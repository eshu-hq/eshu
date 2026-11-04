// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// artifact is one regular file read from the artifact directory with its
// content identity. Identity derives from bytes, never from file names, so a
// rename or a duplicate copy drains as the same generation.
type artifact struct {
	name string
	body []byte
	hash [32]byte
}

// scanReport is the outcome of listing the artifact directory: the readable
// regular files plus the count of entries skipped per hostile class.
type scanReport struct {
	artifacts        []artifact
	skippedIrregular int
	skippedOversize  int
	skippedDuplicate int
	skippedOverCap   int
}

// sourceOptions carries the per-run extraction inputs. Tests inject a stub
// engine; production passes the pinned tesseract backend built in service.go.
type sourceOptions struct {
	engine        ocr.Engine
	sourceSystem  string
	scopePrefix   string
	maxInputBytes int64
	maxArtifacts  int
}

// dirSource adapts an operator-supplied directory of reviewed image artifacts
// to collector.Source: one collected generation per distinct file, drained in
// sorted name order. It implements collector.Source.
type dirSource struct {
	options   sourceOptions
	artifacts []artifact
	index     int
	logger    *slog.Logger
}

// compile-time proof that the adapter satisfies the shared commit boundary.
var _ collector.Source = (*dirSource)(nil)

// newDirSource scans the artifact directory and fails closed when it cannot
// be listed. An empty directory drains to zero generations without an error.
func newDirSource(dir string, options sourceOptions, logger *slog.Logger) (*dirSource, error) {
	if options.engine == nil {
		return nil, fmt.Errorf("document import source requires an OCR engine")
	}
	report, err := scanArtifactDir(dir, options.maxInputBytes, options.maxArtifacts)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("document import artifacts scanned",
		"artifact_dir", dir,
		"artifacts", len(report.artifacts),
		"skipped_irregular", report.skippedIrregular,
		"skipped_oversize", report.skippedOversize,
		"skipped_duplicate", report.skippedDuplicate,
		"skipped_over_cap", report.skippedOverCap,
	)
	return &dirSource{options: options, artifacts: report.artifacts, logger: logger}, nil
}

// scanArtifactDir lists regular files in sorted order with bounded reads.
// Symlinks, directories, devices, oversize files, duplicate content, and
// entries past the artifact cap are skipped and counted, never committed.
func scanArtifactDir(dir string, maxBytes int64, maxArtifacts int) (scanReport, error) {
	var report scanReport
	entries, err := os.ReadDir(dir)
	if err != nil {
		return scanReport{}, fmt.Errorf("list artifact dir: %w", err)
	}
	if maxBytes <= 0 {
		return scanReport{}, fmt.Errorf("max input bytes must be positive")
	}
	seen := make(map[[32]byte]bool)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		clean := filepath.Clean(name)
		if clean != name || filepath.IsAbs(name) || name == "." || name == ".." {
			report.skippedIrregular++
			continue
		}
		path := filepath.Join(dir, name)
		if rel, err := filepath.Rel(dir, path); err != nil || rel != name {
			report.skippedIrregular++
			continue
		}
		if !isRegularLink(path) {
			report.skippedIrregular++
			continue
		}
		body, regular, err := readRegularFile(path, maxBytes)
		if err != nil {
			return scanReport{}, err
		}
		if !regular {
			report.skippedIrregular++
			continue
		}
		if body == nil {
			report.skippedOversize++
			continue
		}
		hash := sha256.Sum256(body)
		if seen[hash] {
			report.skippedDuplicate++
			continue
		}
		seen[hash] = true
		if maxArtifacts > 0 && len(report.artifacts) >= maxArtifacts {
			report.skippedOverCap++
			continue
		}
		report.artifacts = append(report.artifacts, artifact{name: name, body: body, hash: hash})
	}
	return report, nil
}

// isRegularLink reports whether path is a regular file without following a
// symlink: links, directories, and devices all report false. Lstat runs
// before open so a pre-existing symlink never reaches the reader.
func isRegularLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// readRegularFile opens path (already gated by isRegularLink) and reads at
// most maxBytes+1 to detect oversize inputs. It re-checks regular-file status
// after open and reports false for anything else; a nil body with regular
// true means the content exceeds the cap.
func readRegularFile(path string, maxBytes int64) (body []byte, regular bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open artifact: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	limited := io.LimitReader(file, maxBytes+1)
	body, err = io.ReadAll(limited)
	if err != nil {
		return nil, false, fmt.Errorf("read artifact: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, true, nil
	}
	return body, true, nil
}

// Next extracts the next artifact and adapts its envelopes to one collected
// generation. It returns ok=false when the directory is drained. A cancelled
// context fails before extraction and before any commit, so no partial
// generation is persisted.
func (s *dirSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if err := ctx.Err(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.index >= len(s.artifacts) {
		return collector.CollectedGeneration{}, false, nil
	}
	item := s.artifacts[s.index]
	s.index++
	if err := ctx.Err(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	result, err := ocr.Extract(ctx, s.request(item))
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("extract %q: %w", item.name, err)
	}
	hashHex := hex.EncodeToString(item.hash[:])
	generationID := "dococr-" + hashHex
	scopeID := s.options.scopePrefix + "-" + hashHex[:16]
	observedAt := time.Now().UTC()
	collectedScope := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  s.options.sourceSystem,
		ScopeKind:     scope.KindDocumentationSource,
		CollectorKind: scope.CollectorDocumentation,
		PartitionKey:  scopeID,
		Metadata: map[string]string{
			"artifact_name": item.name,
			"source_hash":   hashHex,
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return collector.FactsFromSlice(collectedScope, generation, result.Envelopes), true, nil
}

// request builds the OCR request for one artifact. Identity fields derive
// from the content hash so re-runs over identical bytes name the same
// generation; human labels carry the operator-supplied file name.
func (s *dirSource) request(item artifact) ocr.Request {
	hashHex := hex.EncodeToString(item.hash[:])
	return ocr.Request{
		ScopeID:        s.options.scopePrefix + "-" + hashHex[:16],
		GenerationID:   "dococr-" + hashHex,
		ObservedAt:     time.Now().UTC(),
		SourceSystem:   s.options.sourceSystem,
		SourceURI:      "document-import/" + item.name,
		SourceRecordID: hashHex,
		SourceName:     item.name,
		SourceID:       s.options.scopePrefix + "-" + hashHex[:16],
		DocumentID:     s.options.scopePrefix + ":" + hashHex,
		ExternalID:     hashHex,
		RevisionID:     "dococr-" + hashHex,
		Title:          item.name,
		Body:           item.body,
		Engine:         s.options.engine,
	}
}

// drainOnce commits every generation from src in order and returns the
// committed generation and envelope counts. The first error stops the drain;
// committed generations stay committed, unstarted ones never begin.
func drainOnce(
	ctx context.Context,
	src collector.Source,
	committer collector.Committer,
	logger *slog.Logger,
) (generations int, envelopes int, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	for {
		if err := ctx.Err(); err != nil {
			return generations, envelopes, err
		}
		collected, ok, err := src.Next(ctx)
		if err != nil {
			return generations, envelopes, err
		}
		if !ok {
			return generations, envelopes, nil
		}
		count := collected.EstimatedFactCount
		if err := committer.CommitScopeGeneration(ctx, collected.Scope, collected.Generation, collected.Facts); err != nil {
			return generations, envelopes, fmt.Errorf("commit generation %q: %w", collected.Generation.GenerationID, err)
		}
		generations++
		envelopes += count
		if id := strings.TrimSpace(collected.Generation.GenerationID); id != "" {
			logger.Info("document import generation committed",
				"generation_id", id,
				"scope_id", collected.Scope.ScopeID,
				"envelopes", count,
			)
		}
	}
}
