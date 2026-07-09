// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay"
)

// RecordOptions configures one record pass.
type RecordOptions struct {
	// Emitter is the parser-fact source to record. Required. It is drained for one
	// batch; the recorded fixture captures every envelope it yields.
	Emitter *Emitter
	// Path is the output fixture location (required).
	Path string
	// RepoRoot, when set, makes the written fixture portable: every occurrence of
	// this absolute repository root (in provenance URIs and in the parser-embedded
	// payload paths) is replaced with a sentinel so a committed fixture carries no
	// machine-specific checkout path. Leave empty for a non-portable temp-dir
	// recording (the round-trip test's existing use). A portable fixture is replayed
	// with NewSourceRehydrated / LoadFileRehydrated.
	RepoRoot string
}

// Record drains the emitter for one batch, captures every parser-emitted fact
// envelope (with provenance), and writes the batch as a canonical parser fixture
// to opts.Path. Because the real parser and the real collector envelope seam
// produce the envelopes, every derived field — the FactID from facts.StableID
// and the full SourceRef provenance — is captured with production fidelity. The
// written fixture is canonical (sorted keys, generation_id derived, observed_at
// collapsed, parser payload preserved verbatim) so re-recording the same tree
// yields byte-identical output, and it replays credential-free through NewSource.
func Record(ctx context.Context, opts RecordOptions) error {
	if opts.Emitter == nil {
		return errors.New("parserfixture: emitter is required")
	}
	if strings.TrimSpace(opts.Path) == "" {
		return errors.New("parserfixture: output path is required")
	}

	var captured *captureState
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("parserfixture: %w", err)
		}
		gen, ok, err := opts.Emitter.Next(ctx)
		if err != nil {
			return fmt.Errorf("parserfixture: emit generation: %w", err)
		}
		if !ok {
			break
		}
		if captured != nil {
			return errors.New("parserfixture: emitter yielded more than one generation; the fixture records a single scope")
		}
		captured = captureGeneration(opts.Emitter, gen)
	}
	if captured == nil {
		return errors.New("parserfixture: emitter produced no generation; nothing to record")
	}
	return writeFixture(*captured, opts.Path, opts.RepoRoot)
}

// captureState holds the one captured scope before serialization.
type captureState struct {
	language string
	scope    Scope
}

// captureGeneration drains one collected generation's fact channel and maps it
// onto the fixture scope schema.
func captureGeneration(e *Emitter, gen collector.CollectedGeneration) *captureState {
	recorded := make([]Fact, 0, gen.FactCount())
	for env := range gen.Facts {
		recorded = append(recorded, toFact(env))
	}
	return &captureState{
		language: strings.TrimSpace(gen.Scope.SourceSystem),
		scope: Scope{
			ScopeID:       gen.Scope.ScopeID,
			SourceSystem:  gen.Scope.SourceSystem,
			ScopeKind:     string(gen.Scope.ScopeKind),
			CollectorKind: string(gen.Scope.CollectorKind),
			GenerationID:  gen.Generation.GenerationID,
			ObservedAt:    gen.Generation.ObservedAt,
			RepoID:        e.opts.RepoID,
			Facts:         recorded,
		},
	}
}

// toFact maps one emitted envelope onto the fixture fact schema, preserving every
// durable field and the full provenance. SourceURI is recorded verbatim; the
// source-record id is recorded only when it diverges from the stable fact key
// (the file-fact emitter sets them equal), to keep the fixture clean while
// preserving real provenance when a future emitter sets a distinct id.
func toFact(env facts.Envelope) Fact {
	return Fact{
		FactKind:         env.FactKind,
		StableFactKey:    env.StableFactKey,
		SchemaVersion:    env.SchemaVersion,
		CollectorKind:    env.CollectorKind,
		FencingToken:     env.FencingToken,
		SourceConfidence: env.SourceConfidence,
		Payload:          env.Payload,
		SourceSystem:     recordedSourceSystem(env),
		SourceURI:        env.SourceRef.SourceURI,
		SourceRecordID:   recordedSourceRecordID(env),
	}
}

// recordedSourceSystem omits the provenance source system when it equals the
// collector kind (the common case), keeping the fixture compact; a divergent
// value is recorded verbatim.
func recordedSourceSystem(env facts.Envelope) string {
	if env.SourceRef.SourceSystem == env.CollectorKind {
		return ""
	}
	return env.SourceRef.SourceSystem
}

// recordedSourceRecordID omits the source-record id when it equals the stable
// fact key (the parser file-fact default), recording a distinct id verbatim.
func recordedSourceRecordID(env facts.Envelope) string {
	if env.SourceRef.SourceRecordID == env.StableFactKey {
		return ""
	}
	return env.SourceRef.SourceRecordID
}

// writeFixture builds the fixture file, canonicalizes it, optionally portableizes
// it against repoRoot, verifies it still loads as a valid fixture, and writes it
// to disk. The load-back is a guard: the recorder must never emit a fixture the
// replay loader would reject.
func writeFixture(captured captureState, path, repoRoot string) error {
	file := File{
		Language:      captured.language,
		SchemaVersion: SchemaVersionV1,
		Scope:         captured.scope,
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("parserfixture: marshal fixture: %w", err)
	}
	canonical, err := replay.Canonicalize(raw, canonicalOptions())
	if err != nil {
		return fmt.Errorf("parserfixture: canonicalize fixture: %w", err)
	}
	if strings.TrimSpace(repoRoot) != "" {
		canonical, err = portableize(canonical, repoRoot)
		if err != nil {
			return err
		}
	}
	// #nosec G306 -- a parser fixture is a committed, world-readable test fixture,
	// not a secret; 0o644 matches the repo's other generated artifacts.
	if err := os.WriteFile(path, canonical, 0o644); err != nil {
		return fmt.Errorf("parserfixture: write %q: %w", path, err)
	}
	if _, err := ParseAndValidate(canonical); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("parserfixture: recorded fixture is invalid: %w", err)
	}
	return nil
}
