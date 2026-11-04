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

// artifact is one regular file accepted at scan time. Identity derives from
// content bytes, never from file names, so a rename or a duplicate copy
// drains as the same generation. Bodies are never retained: Next re-reads
// each file at drain time, so peak residency is one artifact.
type artifact struct {
	name string
	hash [32]byte
	size int64
}

// scanReport is the outcome of listing the artifact directory: the accepted
// descriptors plus the count of entries skipped per hostile class.
type scanReport struct {
	artifacts        []artifact
	skippedIrregular int
	skippedOversize  int
	skippedDuplicate int
	skippedOverCap   int
}

// extractor is the extraction entry point: it runs the request and records
// extraction telemetry. The pinned tesseract backend implements it with its
// Extract wrapper; tests inject a stub over the same seam.
type extractor interface {
	Extract(context.Context, ocr.Request) (ocr.Result, error)
}

// sourceOptions carries the per-run extraction inputs. Tests inject a stub
// extractor; production passes the pinned tesseract backend built in
// service.go.
type sourceOptions struct {
	engine        extractor
	sourceSystem  string
	scopePrefix   string
	maxInputBytes int64
	maxArtifacts  int
}

// dirSource adapts an operator-supplied directory of reviewed image artifacts
// to collector.Source: one collected generation per distinct file, drained in
// sorted name order. It implements collector.Source.
type dirSource struct {
	dir       string
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
		return nil, fmt.Errorf("document import source requires an OCR extractor")
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
	return &dirSource{dir: dir, options: options, artifacts: report.artifacts, logger: logger}, nil
}

// scanArtifactDir lists regular files in sorted order, hashing each accepted
// body to derive its identity. Symlinks, directories, devices, oversize
// files, duplicate content, and entries past the artifact cap are skipped
// and counted, never committed and never retained in memory.
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
		if err := checkInsideDir(dir, name); err != nil {
			report.skippedIrregular++
			continue
		}
		if maxArtifacts > 0 && len(report.artifacts) >= maxArtifacts {
			report.skippedOverCap++
			continue
		}
		body, err := readVerifiedRegular(filepath.Join(dir, name), maxBytes)
		if err != nil {
			return scanReport{}, err
		}
		if body == nil {
			report.skippedIrregular++
			continue
		}
		if len(body) > int(maxBytes) {
			report.skippedOversize++
			continue
		}
		hash := sha256.Sum256(body)
		if seen[hash] {
			report.skippedDuplicate++
			continue
		}
		seen[hash] = true
		report.artifacts = append(report.artifacts, artifact{name: name, hash: hash, size: int64(len(body))})
	}
	return report, nil
}

// checkInsideDir rejects names that escape the artifact directory.
func checkInsideDir(dir, name string) error {
	rel, err := filepath.Rel(dir, filepath.Join(dir, name))
	if err != nil || rel != name {
		return fmt.Errorf("artifact name %q escapes its directory", name)
	}
	return nil
}

// openVerifiedRegular opens a path that Lstat proves regular, then verifies
// the open file is the same file the Lstat observed. A symlink swapped in
// between the two checks (or any non-regular open) fails instead of reading
// through the link.
func openVerifiedRegular(path string) (*os.File, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}
	if !linkInfo.Mode().IsRegular() {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	openInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat open artifact: %w", err)
	}
	if !openInfo.Mode().IsRegular() || !os.SameFile(linkInfo, openInfo) {
		_ = file.Close()
		return nil, nil
	}
	return file, nil
}

// readVerifiedRegular reads at most maxBytes+1 to detect oversize inputs. It
// returns a nil body when the path is not a stable regular file (skipped,
// never fatal); exceeding the cap surfaces as an over-long body the caller
// counts as oversize.
func readVerifiedRegular(path string, maxBytes int64) (body []byte, err error) {
	file, err := openVerifiedRegular(path)
	if err != nil {
		return nil, err
	}
	if file == nil {
		return nil, nil
	}
	defer func() {
		_ = file.Close()
	}()
	limited := io.LimitReader(file, maxBytes+1)
	body, err = io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	return body, nil
}

// Next re-reads the next artifact, verifies its bytes still match the scanned
// identity, extracts it, and adapts the envelopes to one collected
// generation. It returns ok=false when the directory is drained. A cancelled
// context fails before extraction and before any commit, so no partial
// generation is persisted. A file whose bytes changed since the scan fails
// the run instead of committing under a stale identity.
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
	body, err := readVerifiedRegular(filepath.Join(s.dir, item.name), s.options.maxInputBytes)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("re-read %q: %w", item.name, err)
	}
	if body == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("artifact %q changed shape during the drain", item.name)
	}
	if hash := sha256.Sum256(body); hash != item.hash {
		return collector.CollectedGeneration{}, false, fmt.Errorf("artifact %q bytes changed during the drain", item.name)
	}
	result, err := s.options.engine.Extract(ctx, s.request(item, body))
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
func (s *dirSource) request(item artifact, body []byte) ocr.Request {
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
		Body:           body,
		Options:        ocr.Options{},
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
