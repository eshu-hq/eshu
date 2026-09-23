// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recorder

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
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// Options configures one record pass.
type Options struct {
	// Path is the -cassette-file output location (required).
	Path string
	// CollectorLabel is the informational cassette.File.Collector value, e.g.
	// "kubernetes_live". Not validated at replay time.
	CollectorLabel string
	// RedactKeys are payload key names redacted to the canonical redaction
	// sentinel wherever they appear, for collectors whose facts can carry a
	// secret. Fact payloads are secret-sanitized by their collector (tokens,
	// policy documents, message bodies never reach an envelope), so most
	// collectors pass none; the input tape (R-4) redacts at the HTTP boundary.
	// Secret-sanitized is not identifier-free: account ids, ARNs, names,
	// hostnames and addresses do reach envelopes, which is what Pseudonymize
	// is for.
	RedactKeys []string
	// Pseudonymize, when set, wraps the source with recordpseudo.Wrap so every
	// identifier is replaced by a keyed, structure-preserving pseudonym before
	// the recorder sees it (#6965 Phase 3). The key fingerprint is written to
	// the cassette; the key never is.
	Pseudonymize *recordpseudo.Config
	// RequirePseudonymization refuses to run -- before polling the source --
	// unless Pseudonymize carries a usable key. A collector that records
	// real estates sets it so a missing key can never produce a raw cassette.
	RequirePseudonymization bool
	// OnPseudonymized receives the pseudonymization report after the source
	// is drained (counts, field paths, key fingerprint; never a value). Nil
	// discards it.
	OnPseudonymized func(recordpseudo.Report)
}

// Run performs one credentialed record pass: it polls src for a single batch
// (until Next reports the batch exhausted), captures every emitted fact
// envelope, and writes the batch as a canonical cassette to opts.Path.
//
// Run performs no durable commit, so it needs only the collector's live
// credentials, not a database. Because the real collector produces the
// envelopes, every derived field — most importantly each fact's object_id,
// computed by the collector's real facts.StableID derivation — is captured with
// full fidelity (the structural fix for cassette object_id drift, #3928). The
// written cassette is canonical (sorted keys, volatile fields collapsed,
// generation_id derived, configured secrets redacted) so re-recording the same
// input yields byte-identical output, and it replays credential-free through
// replay/cassette.
//
// Before the canonical bytes reach disk they pass recordpseudo.Verify, the
// private-data membership belt: a candidate identifier that is neither a
// documented safe form nor a pseudonym this run produced aborts the record
// and nothing is written.
func Run(ctx context.Context, src collector.Source, opts Options) error {
	if src == nil {
		return errors.New("recorder: source is required")
	}
	if strings.TrimSpace(opts.Path) == "" {
		return errors.New("recorder: output path is required")
	}
	if opts.RequirePseudonymization && (opts.Pseudonymize == nil || opts.Pseudonymize.Key.IsZero()) {
		return errors.New("recorder: pseudonymization is required but no recording key is configured")
	}
	var rec recording
	if opts.Pseudonymize != nil {
		if err := opts.Pseudonymize.Validate(); err != nil {
			return fmt.Errorf("recorder: %w", err)
		}
		wrapped, report, err := recordpseudo.Wrap(src, opts.Pseudonymize.Key, opts.Pseudonymize.Policy)
		if err != nil {
			return fmt.Errorf("recorder: %w", err)
		}
		src = wrapped
		rec.report = report
		rec.keyFingerprint = opts.Pseudonymize.Key.Fingerprint()
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("recorder: %w", err)
		}
		gen, ok, err := src.Next(ctx)
		if err != nil {
			return fmt.Errorf("recorder: collect generation: %w", err)
		}
		if !ok {
			break
		}
		if err := rec.capture(gen); err != nil {
			return err
		}
	}
	if rec.report != nil && opts.OnPseudonymized != nil {
		opts.OnPseudonymized(*rec.report)
	}
	return rec.write(opts)
}

// recording accumulates the captured scopes for one batch before they are
// serialized. It is single-goroutine: Run drains each generation in turn.
type recording struct {
	scopes []cassette.Scope
	// report and keyFingerprint are set when the run is pseudonymized.
	report         *recordpseudo.Report
	keyFingerprint string
}

// capture drains one collected generation's fact channel and appends the scope.
// Draining fully (rather than streaming) is acceptable because record produces
// a fixture, not a steady-state ingest. A post-stream FactStreamErr aborts the
// whole record so a partial cassette is never written.
func (r *recording) capture(gen collector.CollectedGeneration) error {
	envelopes := make([]facts.Envelope, 0, gen.FactCount())
	for env := range gen.Facts {
		envelopes = append(envelopes, env)
	}
	if gen.FactStreamErr != nil {
		if err := gen.FactStreamErr(); err != nil {
			return fmt.Errorf("recorder: fact stream for scope %q: %w", gen.Scope.ScopeID, err)
		}
	}
	r.scopes = append(r.scopes, toScope(gen, envelopes))
	return nil
}

// write builds the cassette file, canonicalizes it, runs the private-data
// membership belt over the canonical bytes, verifies the result still loads
// as a valid cassette, and writes it to disk. Verify runs BEFORE the write so
// a refused recording leaves no file behind; the load-back is a guard that
// the recorder never emits a cassette the replay loader would reject.
func (r *recording) write(opts Options) error {
	if len(r.scopes) == 0 {
		return errors.New("recorder: no scopes captured; nothing to record")
	}
	file := cassette.File{
		Collector:               opts.CollectorLabel,
		SchemaVersion:           cassette.SchemaVersionV1,
		Scopes:                  r.scopes,
		PseudonymKeyFingerprint: r.keyFingerprint,
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("recorder: marshal cassette: %w", err)
	}
	canonical, err := replay.Canonicalize(raw, replay.DefaultCanonicalOptions().WithRedactedKeys(opts.RedactKeys...))
	if err != nil {
		return fmt.Errorf("recorder: canonicalize cassette: %w", err)
	}
	produced := recordpseudo.Set{}
	if r.report != nil {
		produced = r.report.Produced
	}
	if err := recordpseudo.Verify(canonical, produced); err != nil {
		return fmt.Errorf("recorder: refusing to write %q: %w", opts.Path, err)
	}
	// #nosec G306 -- a cassette is a committed, world-readable test fixture, not
	// a secret; 0o644 matches the repo's other generated artifacts.
	if err := os.WriteFile(opts.Path, canonical, 0o644); err != nil {
		return fmt.Errorf("recorder: write %q: %w", opts.Path, err)
	}
	// Load the output back through the real replay loader: the recorder must
	// never leave behind a cassette the replay path would reject. On failure
	// remove the half-written artifact so a retry starts clean.
	if _, err := cassette.LoadFile(opts.Path); err != nil {
		_ = os.Remove(opts.Path)
		return fmt.Errorf("recorder: recorded cassette is invalid: %w", err)
	}
	return nil
}

// toScope maps one collected generation onto the cassette scope schema. The
// per-fact ObservedAt, ScopeID, GenerationID, and derived FactID are not stored:
// they are re-derived from the scope at replay time, so storing them would be
// redundant churn. observed_at and generation_id are collapsed/derived by
// canonicalization after marshaling.
func toScope(gen collector.CollectedGeneration, envelopes []facts.Envelope) cassette.Scope {
	out := cassette.Scope{
		ScopeID:       gen.Scope.ScopeID,
		SourceSystem:  gen.Scope.SourceSystem,
		ScopeKind:     string(gen.Scope.ScopeKind),
		CollectorKind: string(gen.Scope.CollectorKind),
		PartitionKey:  gen.Scope.PartitionKey,
		Metadata:      gen.Scope.Metadata,
		GenerationID:  gen.Generation.GenerationID,
		ObservedAt:    gen.Generation.ObservedAt,
		TriggerKind:   string(gen.Generation.TriggerKind),
		Facts:         make([]cassette.Fact, 0, len(envelopes)),
	}
	for _, env := range envelopes {
		out.Facts = append(out.Facts, toFact(env))
	}
	return out
}

// toFact maps one emitted envelope onto the cassette fact schema, preserving
// every durable field the collector produced — including the payload verbatim,
// which carries the real object_id.
func toFact(env facts.Envelope) cassette.Fact {
	return cassette.Fact{
		FactKind:         env.FactKind,
		StableFactKey:    env.StableFactKey,
		SchemaVersion:    env.SchemaVersion,
		CollectorKind:    env.CollectorKind,
		FencingToken:     env.FencingToken,
		SourceConfidence: env.SourceConfidence,
		Payload:          env.Payload,
		IsTombstone:      env.IsTombstone,
		SourceURI:        env.SourceRef.SourceURI,
		SourceRecordID:   recordedSourceRecordID(env),
	}
}

// recordedSourceRecordID returns the source-record id to persist. The cassette
// reader defaults an empty source_record_id to the stable fact key, so when the
// collector's SourceRecordID already equals the key it is omitted to keep the
// cassette clean and the round-trip stable; a distinct id (the common case for
// most collectors) is recorded verbatim so replay reproduces real provenance.
func recordedSourceRecordID(env facts.Envelope) string {
	if env.SourceRef.SourceRecordID == env.StableFactKey {
		return ""
	}
	return env.SourceRef.SourceRecordID
}
