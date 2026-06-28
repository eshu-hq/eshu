// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Compile-time proof that the emitter satisfies the shared replay.Source
// contract so it drains through the same drain helper as the replay Source and
// can be handed to Record.
var _ replay.Source = (*Emitter)(nil)

// Parser-fact scope identity constants. Parser file facts are produced by the
// Git collector's emission seam, so the recorded scope mirrors that family.
const (
	// parserFactSourceSystem is the provenance source system for parser file
	// facts, matching collector.ParserFileFactEnvelope's "git" source system.
	parserFactSourceSystem = "git"
	// parserFactCollectorKind is the collector family label for parser file facts.
	parserFactCollectorKind = "git"
	// parserFactScopeKind classifies the recorded scope.
	parserFactScopeKind = "repository"
)

// emitterObservedAt is the fixed observation timestamp the emitter stamps onto
// the recorded generation. A fixed instant keeps the recorded fixture stable
// across re-records (canonicalization would collapse a real timestamp anyway,
// but stamping a fixed value keeps the live-vs-replayed round-trip exact).
var emitterObservedAt = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// EmitterOptions configures one record-time parser-fact emission pass over a
// source tree.
type EmitterOptions struct {
	// ScopeID is the durable scope identity the parser facts are emitted under
	// (required).
	ScopeID string
	// RepoID is the stable repository identity used in fact keys and payloads
	// (required). It must not contain checkout paths, commit SHAs, or hostnames so
	// the recorded fixture stays portable.
	RepoID string
	// TreePath is the source tree the real parser runs over (required).
	TreePath string
	// GenerationID is the durable generation identity. Defaults to a value derived
	// from ScopeID so a re-record is stable.
	GenerationID string
}

// Emitter runs the real parser over a source tree and yields the file-fact
// envelopes the Git collector's emission seam (collector.ParserFileFactEnvelope)
// produces, so a recording captures production envelope shape and provenance
// rather than a re-implementation. Emitter reads the local source tree; it makes
// no network calls and needs no credentials. It is single-goroutine per
// collector.Service; Next is not safe for concurrent use.
type Emitter struct {
	opts     EmitterOptions
	engine   *parser.Engine
	registry parser.Registry
	drained  bool
}

// NewEmitter validates opts, constructs the default parser engine, and returns a
// ready Emitter.
func NewEmitter(opts EmitterOptions) (*Emitter, error) {
	if strings.TrimSpace(opts.ScopeID) == "" {
		return nil, errors.New("parserfixture: scope_id is required")
	}
	if strings.TrimSpace(opts.RepoID) == "" {
		return nil, errors.New("parserfixture: repo_id is required")
	}
	if strings.TrimSpace(opts.TreePath) == "" {
		return nil, errors.New("parserfixture: tree path is required")
	}
	info, err := os.Stat(opts.TreePath)
	if err != nil {
		return nil, fmt.Errorf("parserfixture: stat tree %q: %w", opts.TreePath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("parserfixture: tree %q is not a directory", opts.TreePath)
	}
	// Resolve the tree to an absolute path. parser.Engine.ParsePath stores an
	// absolute file path in each payload, and the Git collector seam relativizes
	// that against the repo root. A relative repo root cannot relativize an
	// absolute file path, so the collector would fall back to filepath.Base and
	// collapse nested same-named files into one stable_fact_key / relative_path
	// (the #4019 nested-path class). Normalizing here keeps provenance correct
	// regardless of the caller's working directory.
	absTree, err := filepath.Abs(opts.TreePath)
	if err != nil {
		return nil, fmt.Errorf("parserfixture: resolve tree %q: %w", opts.TreePath, err)
	}
	opts.TreePath = absTree
	if strings.TrimSpace(opts.GenerationID) == "" {
		// Stamp the canonical generation_id derived from the scope so the live run's
		// generation_id already equals its recorded/replayed form: canonicalization
		// re-derives the same value, so record is a no-op on it and live==replayed.
		opts.GenerationID = replay.DerivedGenerationID(opts.ScopeID)
	}
	registry := parser.DefaultRegistry()
	engine, err := parser.NewEngine(registry, parser.NewRuntime())
	if err != nil {
		return nil, fmt.Errorf("parserfixture: build parser engine: %w", err)
	}
	return &Emitter{opts: opts, engine: engine, registry: registry}, nil
}

// Next yields one CollectedGeneration carrying every parser file fact for the
// tree, then returns ok=false to signal the batch is exhausted (mirroring the
// cassette source's single-batch-per-poll contract).
func (e *Emitter) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if e.drained {
		e.drained = false
		return collector.CollectedGeneration{}, false, nil
	}
	e.drained = true

	envelopes, err := e.emit(ctx)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}

	scopeValue := scope.IngestionScope{
		ScopeID:       e.opts.ScopeID,
		SourceSystem:  parserFactSourceSystem,
		ScopeKind:     parserFactScopeKind,
		CollectorKind: parserFactCollectorKind,
		PartitionKey:  e.opts.ScopeID,
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: e.opts.GenerationID,
		ScopeID:      e.opts.ScopeID,
		ObservedAt:   emitterObservedAt,
		IngestedAt:   emitterObservedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKind("snapshot"),
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), true, nil
}

// emit walks the tree in sorted order, parses each registry-recognized file with
// the real engine, and builds the real file-fact envelope for it. Files the
// registry has no parser for are skipped (the same way the collector skips
// unparseable files). The deterministic walk order plus stable_fact_key sorting
// at canonicalization keep the recording stable.
func (e *Emitter) emit(ctx context.Context) ([]facts.Envelope, error) {
	paths, err := e.parseablePaths()
	if err != nil {
		return nil, err
	}
	envelopes := make([]facts.Envelope, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parserfixture: %w", err)
		}
		parsed, parseErr := e.engine.ParsePath(e.opts.TreePath, path, false, parser.Options{})
		if parseErr != nil {
			// A file the registry claims but the parser cannot read is skipped, not
			// fatal: the recording captures what the parser actually emits.
			continue
		}
		env := collector.ParserFileFactEnvelope(
			e.opts.TreePath,
			e.opts.RepoID,
			e.opts.ScopeID,
			e.opts.GenerationID,
			emitterObservedAt,
			parsed,
			false,
		)
		envelopes = append(envelopes, env)
	}
	if len(envelopes) == 0 {
		return nil, fmt.Errorf("parserfixture: no parseable files under %q", e.opts.TreePath)
	}
	return envelopes, nil
}

// parseablePaths returns the registry-recognized files under the tree in sorted
// order. Sorting makes the emission order deterministic regardless of the
// filesystem's directory iteration order.
func (e *Emitter) parseablePaths() ([]string, error) {
	var paths []string
	walkErr := filepath.WalkDir(e.opts.TreePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := e.registry.LookupByPath(path); !ok {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("parserfixture: walk tree %q: %w", e.opts.TreePath, walkErr)
	}
	sort.Strings(paths)
	return paths, nil
}
